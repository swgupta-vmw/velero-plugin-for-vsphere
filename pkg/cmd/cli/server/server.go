/*
Copyright 2020 the Velero contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"k8s.io/client-go/util/workqueue"

	"github.com/vmware-tanzu/astrolabe/pkg/ivd"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/backupdriver"
	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/cmd"
	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/controller"
	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/dataMover"
	plugin_clientset "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/generated/clientset/versioned"
	pluginInformers "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/generated/informers/externalversions"
	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/snapshotmgr"
	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/utils"
	"github.com/vmware-tanzu/velero/pkg/buildinfo"
	"github.com/vmware-tanzu/velero/pkg/client"
	"github.com/vmware-tanzu/velero/pkg/cmd/util/signals"
	velero_clientset "github.com/vmware-tanzu/velero/pkg/generated/clientset/versioned"
	"github.com/vmware-tanzu/velero/pkg/metrics"
	"github.com/vmware-tanzu/velero/pkg/util/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// the port where prometheus metrics are exposed
	defaultMetricsAddress = ":8085"
	// server's client default qps and burst
	defaultClientQPS   float32 = 20.0
	defaultClientBurst int     = 30

	defaultProfilerAddress         = "localhost:6060"
	defaultInsecureFlag       bool = true
	defaultVCConfigFromSecret bool = true

	defaultControllerWorkers = 1
	defaultBackupWorkers     = 10
	// the default TTL for a backup
	//defaultBackupTTL = 30 * 24 * time.Hour
)

type serverConfig struct {
	metricsAddress     string
	clientQPS          float32
	clientBurst        int
	profilerAddress    string
	formatFlag         *logging.FormatFlag
	vCenter            string
	port               string
	user               string
	clusterId          string
	insecureFlag       bool
	vcConfigFromSecret bool
	master             string
	kubeConfig         string
	resyncPeriod       time.Duration
	workers            int
	retryIntervalStart time.Duration
	retryIntervalMax   time.Duration
}

func NewCommand(name string, f client.Factory) *cobra.Command {
	var (
		logLevelFlag = logging.LogLevelFlag(logrus.InfoLevel)
		config       = serverConfig{
			metricsAddress:     defaultMetricsAddress,
			clientQPS:          defaultClientQPS,
			clientBurst:        defaultClientBurst,
			profilerAddress:    defaultProfilerAddress,
			formatFlag:         logging.NewFormatFlag(),
			port:               utils.DefaultVCenterPort,
			insecureFlag:       defaultInsecureFlag,
			vcConfigFromSecret: defaultVCConfigFromSecret,
			resyncPeriod:       utils.ResyncPeriod,
		}
	)

	var command = &cobra.Command{
		Use:    "server",
		Short:  "Run the " + name + " server",
		Long:   "Run the " + name + " server",
		Hidden: true,
		Run: func(c *cobra.Command, args []string) {
			// go-plugin uses log.Println to log when it's waiting for all plugin processes to complete so we need to
			// set its output to stdout.
			log.SetOutput(os.Stdout)

			logLevel := logLevelFlag.Parse()
			format := config.formatFlag.Parse()

			// Make sure we log to stdout so cloud log dashboards don't show this as an error.
			logrus.SetOutput(os.Stdout)

			// Velero's DefaultLogger logs to stdout, so all is good there.
			logger := logging.DefaultLogger(logLevel, format)

			formatter := new(logrus.TextFormatter)
			formatter.TimestampFormat = time.RFC3339
			formatter.FullTimestamp = true
			logger.SetFormatter(formatter)

			logger.Debugf("setting log-level to %s", strings.ToUpper(logLevel.String()))

			logger.Infof("Starting %s server %s (%s)", name, buildinfo.Version, buildinfo.FormattedGitSHA())

			f.SetBasename(fmt.Sprintf("%s-%s", c.Parent().Name(), c.Name()))

			s, err := newServer(name, f, config, logger)
			cmd.CheckError(err)

			cmd.CheckError(s.run())
		},
	}

	// Common flags
	command.Flags().Var(logLevelFlag, "log-level", fmt.Sprintf("the level at which to log. Valid values are %s.", strings.Join(logLevelFlag.AllowedValues(), ", ")))
	command.Flags().Var(config.formatFlag, "log-format", fmt.Sprintf("the format for log output. Valid values are %s.", strings.Join(config.formatFlag.AllowedValues(), ", ")))
	command.Flags().StringVar(&config.metricsAddress, "metrics-address", config.metricsAddress, "the address to expose prometheus metrics")
	command.Flags().Float32Var(&config.clientQPS, "client-qps", config.clientQPS, "maximum number of requests per second by the server to the Kubernetes API once the burst limit has been reached")
	command.Flags().IntVar(&config.clientBurst, "client-burst", config.clientBurst, "maximum number of requests by the server to the Kubernetes API in a short period of time")
	command.Flags().StringVar(&config.profilerAddress, "profiler-address", config.profilerAddress, "the address to expose the pprof profiler")
	command.Flags().StringVar(&config.master, "master", config.master, "Master URL to build a client config from. Either this or kubeconfig needs to be set if the pod is being run out of cluster.")
	command.Flags().StringVar(&config.kubeConfig, "kubeconfig", config.kubeConfig, "Absolute path to the kubeconfig. Either this or master needs to be set if the pod is being run out of cluster.")
	command.Flags().DurationVar(&config.resyncPeriod, "resync-period", config.resyncPeriod, "Resync period for cache")

	switch name {
	case utils.DataManagerForPlugin:
		command.Flags().StringVar(&config.vCenter, "vcenter-address", config.vCenter, "VirtualCenter address. If specified, --use-secret should be set to False.")
		command.Flags().StringVar(&config.port, "vcenter-port", config.port, "VirtualCenter port. If specified, --use-secret should be set to False.")
		command.Flags().StringVar(&config.user, "vcenter-user", config.user, "VirtualCenter user. If specified, --use-secret should be set to False.")
		command.Flags().StringVar(&config.clusterId, "cluster-id", config.clusterId, "kubernetes cluster id. If specified, --use-secret should be set to False.")
		command.Flags().BoolVar(&config.insecureFlag, "insecure-Flag", config.insecureFlag, "insecure flag. If specified, --use-secret should be set to False.")
		command.Flags().BoolVar(&config.vcConfigFromSecret, "use-secret", config.vcConfigFromSecret, "retrieve VirtualCenter configuration from secret")

	case utils.BackupDriverForPlugin:
		command.Flags().IntVar(&config.workers, "backup-workers", config.clientBurst, "Concurrency to process multiple backup requests")
		command.Flags().DurationVar(&config.retryIntervalStart, "backup-retry-int-start", config.retryIntervalStart, "Initial retry interval of failed backup request. It exponentially increases with each failure, up to retry-interval-max.")
		command.Flags().DurationVar(&config.retryIntervalMax, "backup-retry-int-max", config.retryIntervalMax, "Maximum retry interval of failed backup request.")

	default:
		fmt.Println("Invalid install package %s", name)
	}

	return command
}

type server struct {
	namespace             string
	pkgName               string
	metricsAddress        string
	kubeClientConfig      *rest.Config
	kubeClient            kubernetes.Interface
	veleroClient          velero_clientset.Interface
	pluginClient          plugin_clientset.Interface
	pluginInformerFactory pluginInformers.SharedInformerFactory
	kubeInformerFactory   kubeinformers.SharedInformerFactory
	ctx                   context.Context
	cancelFunc            context.CancelFunc
	logger                logrus.FieldLogger
	logLevel              logrus.Level
	metrics               *metrics.ServerMetrics
	config                serverConfig
	dataMover             *dataMover.DataMover
	snapManager           *snapshotmgr.SnapshotManager
}

func (s *server) run() error {
	s.logger.Infof("%s server is up and running", s.pkgName)
	signals.CancelOnShutdown(s.cancelFunc, s.logger)

	if s.config.profilerAddress != "" {
		go s.runProfiler()
	}

	// Since s.namespace, which specifies where backups/restores/schedules/etc. should live,
	// *could* be different from the namespace where the Velero server pod runs, check to make
	// sure it exists, and fail fast if it doesn't.
	if err := s.namespaceExists(s.namespace); err != nil {
		return err
	}

	if err := s.runControllers(); err != nil {
		return err
	}

	return nil
}

// Assign vSphere VC credentials and configuration parameters
func getVCConfigParams(config serverConfig, params map[string]interface{}, logger logrus.FieldLogger) error {

	if config.vCenter == "" {
		return errors.New("getVCConfigParams: parameter vcenter-address not provided")
	}
	params[ivd.HostVcParamKey] = config.vCenter

	if config.user == "" {
		return errors.New("getVCConfigParams: parameter vcenter-user not provided")
	}
	params[ivd.UserVcParamKey] = config.user

	passwd := os.Getenv("VC_PASSWORD")
	if passwd == "" {
		logger.Warnf("getVCConfigParams: Environment variable VC_PASSWORD not set or empty")
	}
	params[ivd.PasswordVcParamKey] = passwd

	if config.clusterId == "" {
		return errors.New("getVCConfigParams: parameter vcenter-user not provided")
	}
	params[ivd.ClusterVcParamKey] = config.clusterId

	// Below vc configuration params are optional
	params[ivd.PortVcParamKey] = config.port
	params[ivd.InsecureFlagVcParamKey] = strconv.FormatBool(config.insecureFlag)

	return nil
}

func buildConfig(master, kubeConfig string, f client.Factory) (*rest.Config, error) {
	var config *rest.Config
	var err error
	if master != "" || kubeConfig != "" {
		config, err = clientcmd.BuildConfigFromFlags(master, kubeConfig)
	} else {
		config, err = f.ClientConfig()
	}
	if err != nil {
		return nil, errors.Errorf("failed to create config: %v", err)
	}
	return config, nil
}

func newServer(name string, f client.Factory, config serverConfig, logger *logrus.Logger) (*server, error) {
	logger.Infof("%s server is started", name)
	kubeClient, err := f.KubeClient()
	if err != nil {
		return nil, err
	}

	veleroClient, err := f.Client()
	if err != nil {
		return nil, err
	}

	clientConfig, err := buildConfig(config.master, config.kubeConfig, f)
	if err != nil {
		return nil, err
	}

	pluginClient, err := plugin_clientset.NewForConfig(clientConfig)
	if err != nil {
		return nil, err
	}

	pluginInformerFactory := pluginInformers.NewSharedInformerFactoryWithOptions(pluginClient, config.resyncPeriod, pluginInformers.WithNamespace(f.Namespace()))
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

	ctx, cancelFunc := context.WithCancel(context.Background())

	switch name {
	case utils.DataManagerForPlugin:
		configParams := make(map[string]interface{})
		if config.vcConfigFromSecret == true {
			logger.Infof("VC Configuration will be retrieved from cluster secret")
		} else {
			err := getVCConfigParams(config, configParams, logger)
			if err != nil {
				return nil, err
			}

			logger.Infof("VC configuration provided by user for :%s", configParams[ivd.HostVcParamKey])
		}

		dataMover, err := dataMover.NewDataMoverFromCluster(configParams, logger)
		if err != nil {
			return nil, err
		}

		snapshotMgrConfig := make(map[string]string)
		snapshotMgrConfig[utils.VolumeSnapshotterManagerLocation] = utils.VolumeSnapshotterDataServer
		snapshotmgr, err := snapshotmgr.NewSnapshotManagerFromCluster(configParams, snapshotMgrConfig, logger)
		if err != nil {
			return nil, err
		}

		s := &server{
			namespace:             f.Namespace(),
			pkgName:               name,
			metricsAddress:        config.metricsAddress,
			kubeClient:            kubeClient,
			veleroClient:          veleroClient,
			pluginClient:          pluginClient,
			pluginInformerFactory: pluginInformerFactory,
			kubeInformerFactory:   kubeInformerFactory,
			ctx:                   ctx,
			cancelFunc:            cancelFunc,
			logger:                logger,
			logLevel:              logger.Level,
			config:                config,
			dataMover:             dataMover,
			snapManager:           snapshotmgr,
		}
		return s, nil

	case utils.BackupDriverForPlugin:
		// TODO: If CLUSTER_FLAVOR is GUEST_CLUSTER, set up svcKubeClient to communicate with the Supervisor Cluster
		// If CLUSTER_FLAVOR is WORKLOAD, it is a Supervisor Cluster.
		// By default we are in the Vanilla Cluster

		// TODO: Check if needed for Datamgr
		snapshotMgrConfig := make(map[string]string)
		configParams := make(map[string]interface{})
		snapshotMgrConfig[utils.VolumeSnapshotterManagerLocation] = utils.VolumeSnapshotterDataServer
		snapshotmgr, err := snapshotmgr.NewSnapshotManagerFromCluster(configParams, snapshotMgrConfig, logger)
		if err != nil {
			return nil, err
		}

		s := &server{
			namespace:             f.Namespace(),
			pkgName:               name,
			metricsAddress:        config.metricsAddress,
			kubeClient:            kubeClient,
			veleroClient:          veleroClient,
			pluginClient:          pluginClient,
			pluginInformerFactory: pluginInformerFactory,
			kubeInformerFactory:   kubeInformerFactory,
			ctx:                   ctx,
			cancelFunc:            cancelFunc,
			logger:                logger,
			logLevel:              logger.Level,
			config:                config,
			snapManager:           snapshotmgr,
		}
		return s, nil

	default:
		logger.Error("Invalid install package %s", name)
		errMsg := fmt.Sprintf("Invalid install package %s", name)
		return nil, errors.New(errMsg)
	}
}

// namespaceExists returns nil if namespace can be successfully
// gotten from the kubernetes API, or an error otherwise.
func (s *server) namespaceExists(namespace string) error {
	s.logger.WithField("namespace", namespace).Info("Checking existence of namespace")

	if _, err := s.kubeClient.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{}); err != nil {
		return errors.WithStack(err)
	}

	s.logger.WithField("namespace", namespace).Info("Namespace exists")
	return nil
}

func (s *server) runProfiler() {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	if err := http.ListenAndServe(s.config.profilerAddress, mux); err != nil {
		s.logger.WithError(errors.WithStack(err)).Error("error running profiler http server")
	}
}

func (s *server) runControllers() error {
	s.logger.Info("Starting %s controllers", s.pkgName)

	ctx := s.ctx
	var wg sync.WaitGroup
	// Register controllers
	s.logger.Info("Registering controllers")

	switch s.pkgName {
	case utils.DataManagerForPlugin:
		uploadController := controller.NewUploadController(
			s.logger,
			s.pluginInformerFactory.Veleroplugin().V1().Uploads(),
			s.pluginClient.VeleropluginV1(),
			s.kubeClient,
			s.dataMover,
			s.snapManager,
			os.Getenv("NODE_NAME"),
		)

		downloadController := controller.NewDownloadController(
			s.logger,
			s.pluginInformerFactory.Veleroplugin().V1().Downloads(),
			s.pluginClient.VeleropluginV1(),
			s.kubeClient,
			s.dataMover,
			os.Getenv("NODE_NAME"),
		)

		wg.Add(1)
		go func() {
			defer wg.Done()
			uploadController.Run(s.ctx, 1)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			downloadController.Run(s.ctx, 1)
		}()

	case utils.BackupDriverForPlugin:
		backupDriverController := backupdriver.NewBackupDriverController(
			"BackupDriverController",
			s.logger,
			s.kubeClient,
			utils.ResyncPeriod,
			s.kubeInformerFactory,
			s.pluginInformerFactory,
			workqueue.NewItemExponentialFailureRateLimiter(s.config.retryIntervalStart, s.config.retryIntervalMax))

		wg.Add(1)
		go func() {
			defer wg.Done()
			backupDriverController.Run(s.ctx, s.config.workers)
		}()

	default:
		s.logger.Error("Invalid install package %s", s.pkgName)
		return nil
	}

	// SHARED INFORMERS HAVE TO BE STARTED AFTER ALL CONTROLLERS
	go s.pluginInformerFactory.Start(ctx.Done())
	go s.kubeInformerFactory.Start(ctx.Done())

	s.logger.Info("Server started successfully")

	<-ctx.Done()

	s.logger.Info("Waiting for all controllers to shut down gracefully")
	wg.Wait()

	return nil
}
