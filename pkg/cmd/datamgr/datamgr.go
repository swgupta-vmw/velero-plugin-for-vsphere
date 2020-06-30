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

package datamgr

import (
	"flag"
	"fmt"
	"os"

	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/cmd/cli/install"
	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/cmd/server"

	"github.com/spf13/cobra"
	"k8s.io/klog"

	"github.com/vmware-tanzu/velero/pkg/client"
	//"github.com/vmware-tanzu/velero/pkg/features"
)

func NewCommand(name string) *cobra.Command {
	// Load the config here so that we can extract features from it.
	config, err := client.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Error reading config file: %v\n", err)
	}

	var shortDesc, longDesc string

	if name == "data-manager-for-plugin" {
		shortDesc = "Upload and download snapshots of persistent volume on vSphere kubernetes cluster"
		longDesc = `Data manager is a component in Velero vSphere plugin for
			moving local snapshotted data from/to remote durable persistent storage. 
			Specifically, the data manager component is supposed to be running
			in separate container from the velero server`
	} else {
		shortDesc = "Create, Clone and Delete snapshots on vSphere kubernetes cluster"
		longDesc = `Backup driver is a component in Velero vSphere plugin for
			creating, cloning and deleting snapshots on vsphere storage. It does not move the
		    snapshot data between the local and remote durable storage, but creates CRs for
		    those tasks. The backup driver runs in separate container from the velero server`
	}

	c := &cobra.Command{
		Use:   name,
		Short: shortDesc,
		Long:  longDesc,
	}

	f := client.NewFactory(name, config)
	f.BindFlags(c.PersistentFlags())

	c.AddCommand(
		server.NewCommand(f),
		install.NewCommand(f),
	)

	// init and add the klog flags
	klog.InitFlags(flag.CommandLine)
	c.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	return c
}
