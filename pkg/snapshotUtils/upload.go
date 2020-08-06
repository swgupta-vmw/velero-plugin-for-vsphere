package snapshotUtils

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	backupdriverv1 "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/apis/backupdriver/v1"
	veleropluginv1 "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/apis/veleroplugin/v1"
	backupdriverclientset "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/generated/clientset/versioned/typed/backupdriver/v1"
	veleropluginclientset "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/generated/clientset/versioned/typed/veleroplugin/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
)

/*
This will wait until one of the specified waitForPhases is reached, the timeout is reached or the context is canceled.

Note: Canceling the context cancels the wait, it does not cancel the snapshot operation.  Use CancelSnapshot to cancel
an in-progress snapshot
*/

func checkUploadPhasesAndSendResult(waitForPhases []veleropluginv1.UploadPhase, upload *veleropluginv1.Upload,
	results chan waitResult) {
	for _, checkPhase := range waitForPhases {
		if upload.Status.Phase == checkPhase {
			results <- waitResult{
				item: upload,
				err:  nil,
			}
		}
	}
}

func WaitForUploadPhases(
	ctx context.Context,
	clientSet *veleropluginclientset.VeleropluginV1Client,
	uploadToWait veleropluginv1.Upload,
	waitForPhases []veleropluginv1.UploadPhase,
	namespace string,
	logger logrus.FieldLogger) (*veleropluginv1.Upload, error) {

	results := make(chan waitResult)
	watchlist := cache.NewListWatchFromClient(clientSet.RESTClient(), "uploads", namespace,
		fields.Everything())
	_, controller := cache.NewInformer(
		watchlist,
		&veleropluginv1.Upload{},
		time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				upload := obj.(*veleropluginv1.Upload)
				if upload.Name != uploadToWait.Name {
					return
				}
				logger.Infof("upload added: %v", upload)
				logger.Infof("phase = %s", upload.Status.Phase)
				checkUploadPhasesAndSendResult(waitForPhases, upload, results)
			},
			DeleteFunc: func(obj interface{}) {
				upload := obj.(*veleropluginv1.Upload)
				if upload.Name != uploadToWait.Name {
					return
				}
				logger.Infof("upload deleted: %s", obj)
				results <- waitResult{
					item: nil,
					err:  errors.New("upload deleted"),
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				upload := newObj.(*veleropluginv1.Upload)
				if upload.Name != uploadToWait.Name {
					return
				}
				logger.Infof("upload changed: %v", upload)
				logger.Infof("phase = %s", upload.Status.Phase)
				checkUploadPhasesAndSendResult(waitForPhases, upload, results)

			},
		},
	)
	stop := make(chan struct{})
	go controller.Run(stop)
	select {
	case <-ctx.Done():
		stop <- struct{}{}
		return nil, ctx.Err()
	case result := <-results:
		return result.item.(*veleropluginv1.Upload), result.err
	}
}

// Watch the upload CR for upload status and update it in the snapshot CR.
// Return in case of error or if a terminal state is reached
func WatchUploadAndUpdateSnapshotStatus(
	ctx context.Context,
	snapshot *backupdriverv1.Snapshot,
	uploadName string,
	uploadNamespace string,
	backupdriverClientSet *backupdriverclientset.BackupdriverV1Client,
	veleropluginClientSet *veleropluginclientset.VeleropluginV1Client,
	logger logrus.FieldLogger) error {

	upload, err := veleropluginClientSet.Uploads(uploadNamespace).Get(uploadName, metav1.GetOptions{})
	if err != nil {
		logger.WithError(err).Errorf("Did not find the upload %s", uploadName)
		return err
	}

	// List of upload phases to watch.
	waitPhases := []veleropluginv1.UploadPhase{veleropluginv1.UploadPhaseInProgress, veleropluginv1.UploadPhaseCompleted,
		veleropluginv1.UploadPhaseUploadError, veleropluginv1.UploadPhaseCanceling, veleropluginv1.UploadPhaseCanceled}

	// List of terminal phases. If we reach one of these phases, return.
	terminalPhases := []backupdriverv1.SnapshotPhase{backupdriverv1.SnapshotPhaseUploaded, backupdriverv1.SnapshotPhaseUploadFailed, backupdriverv1.SnapshotPhaseCanceled}

	// Wait for the various upload status phase updates. If we reach a terminal state, return.
	// If not, update the snapshot with the new state and wait for remaining states.
	for true {
		logger.Infof("Snapshot %s/%s Waiting for upload phases %v", snapshot.Namespace, snapshot.Name, waitPhases)
		upload, err = WaitForUploadPhases(ctx, veleropluginClientSet, *upload, waitPhases, uploadNamespace, logger)
		if err != nil {
			logger.WithError(err).Errorf("Failed to wait for updated upload %s", uploadName)
			return err
		}

		var newSnapshotPhase backupdriverv1.SnapshotPhase
		switch upload.Status.Phase {
		case veleropluginv1.UploadPhaseInProgress:
			newSnapshotPhase = backupdriverv1.SnapshotPhaseUploading
		case veleropluginv1.UploadPhaseCompleted:
			newSnapshotPhase = backupdriverv1.SnapshotPhaseUploaded
		case veleropluginv1.UploadPhaseUploadError:
			newSnapshotPhase = backupdriverv1.SnapshotPhaseUploadFailed
		case veleropluginv1.UploadPhaseCanceling:
			newSnapshotPhase = backupdriverv1.SnapshotPhaseCanceled
		case veleropluginv1.UploadPhaseCanceled:
			newSnapshotPhase = backupdriverv1.SnapshotPhaseCanceled
		}

		err = updateSnapshotStatusPhase(snapshot, newSnapshotPhase, backupdriverClientSet)
		if err != nil {
			logger.WithError(err).Errorf("Failed to update snapshot %s/%s to phase %s", snapshot.Namespace, snapshot.Name, newSnapshotPhase)
			return err
		}

		logger.Infof("Snapshot %s/%s status phase updated to %s", snapshot.Namespace, snapshot.Name, newSnapshotPhase)

		for _, p := range terminalPhases {
			if p == newSnapshotPhase {
				// we reached a terminal phase, return
				logger.Infof("Snapshot %s/%s reached terminal phase %s", snapshot.Namespace, snapshot.Name, newSnapshotPhase)
				return nil
			}
		}

		// Not yet reached a terminal phase. Remove the new phase from the wait phases and retry
		for i, p := range waitPhases {
			if p == upload.Status.Phase {
				// we found the wait phase to remove.
				waitPhases = append(waitPhases[:i], waitPhases[i+1:]...)
				break
			}
		}
	}
	return nil
}
