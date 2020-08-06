package snapshotUtils

import (
	"context"
	"time"

	"github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/builder"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	backupdriverv1 "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/apis/backupdriver/v1"
	v1 "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/generated/clientset/versioned/typed/backupdriver/v1"
	core_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"
)

/*
This will wait until one of the specified waitForPhases is reached, the timeout is reached or the context is canceled.

Note: Canceling the context cancels the wait, it does not cancel the snapshot operation.  Use CancelSnapshot to cancel
an in-progress snapshot
*/

type waitResult struct {
	item interface{}
	err  error
}

type BackupRepository struct {
	backupRepositoryName string
}

func NewBackupRepository(backupRepositoryName string) *BackupRepository {
	return &BackupRepository{
		backupRepositoryName,
	}
}

func checkPhasesAndSendResult(waitForPhases []backupdriverv1.SnapshotPhase, snapshot *backupdriverv1.Snapshot,
	results chan waitResult) {
	for _, checkPhase := range waitForPhases {
		if snapshot.Status.Phase == checkPhase {
			results <- waitResult{
				item: snapshot,
				err:  nil,
			}
		}
	}
}

// Create a Snapshot record in the specified namespace.
func SnapshotRef(ctx context.Context,
	clientSet *v1.BackupdriverV1Client,
	objectToSnapshot core_v1.TypedLocalObjectReference,
	namespace string,
	repository BackupRepository,
	waitForPhases []backupdriverv1.SnapshotPhase,
	logger logrus.FieldLogger) (*backupdriverv1.Snapshot, error) {

	snapshotUUID, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Wrapf(err, "Could not create snapshot UUID")
	}
	snapshotName := "snap-" + snapshotUUID.String()
	snapshotReq := builder.ForSnapshot(namespace, snapshotName).
		BackupRepository(repository.backupRepositoryName).
		ObjectReference(objectToSnapshot).
		CancelState(false).Result()

	writtenSnapshot, err := clientSet.Snapshots(namespace).Create(snapshotReq)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to create snapshot record")
	}
	logger.Infof("Snapshot record, %s, created", writtenSnapshot.Name)

	writtenSnapshot.Status.Phase = backupdriverv1.SnapshotPhaseNew
	writtenSnapshot, err = clientSet.Snapshots(namespace).UpdateStatus(writtenSnapshot)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to update status of snapshot record")
	}
	logger.Infof("Snapshot record, %s, status updated to %s", writtenSnapshot.Name, writtenSnapshot.Status.Phase)

	updatedSnapshot, err := WaitForPhases(ctx, clientSet, *writtenSnapshot, waitForPhases, namespace, logger)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to wait for expected snapshot phases")
	}

	return updatedSnapshot, err
}

func WaitForPhases(ctx context.Context, clientSet *v1.BackupdriverV1Client, snapshotToWait backupdriverv1.Snapshot, waitForPhases []backupdriverv1.SnapshotPhase, namespace string, logger logrus.FieldLogger) (*backupdriverv1.Snapshot, error) {
	results := make(chan waitResult)
	watchlist := cache.NewListWatchFromClient(clientSet.RESTClient(), "snapshots", namespace,
		fields.Everything())
	_, controller := cache.NewInformer(
		watchlist,
		&backupdriverv1.Snapshot{},
		time.Second*0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				snapshot := obj.(*backupdriverv1.Snapshot)
				if snapshot.Name != snapshotToWait.Name {
					return
				}
				logger.Infof("snapshot added: %v", snapshot)
				logger.Infof("phase = %s", snapshot.Status.Phase)
				checkPhasesAndSendResult(waitForPhases, snapshot, results)
			},
			DeleteFunc: func(obj interface{}) {
				snapshot := obj.(*backupdriverv1.Snapshot)
				if snapshot.Name != snapshotToWait.Name {
					return
				}
				logger.Infof("snapshot deleted: %s", obj)
				results <- waitResult{
					item: nil,
					err:  errors.New("Snapshot deleted"),
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				snapshot := newObj.(*backupdriverv1.Snapshot)
				if snapshot.Name != snapshotToWait.Name {
					return
				}
				logger.Infof("snapshot changed: %v", snapshot)
				logger.Infof("phase = %s", snapshot.Status.Phase)
				checkPhasesAndSendResult(waitForPhases, snapshot, results)

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
		return result.item.(*backupdriverv1.Snapshot), result.err
	}
}

// Watch the supervisor snapshot CR for upload status and update it in the snapshot CR.
// Return in case of error or if a terminal state is reached
func WatchSvcSnapshotAndUpdateSnapshotStatus(
	ctx context.Context,
	snapshot *backupdriverv1.Snapshot,
	clientSet *v1.BackupdriverV1Client,
	svcSnapshotName string,
	svcNamespace string,
	svcClientSet *v1.BackupdriverV1Client,
	logger logrus.FieldLogger) error {

	// List of supervisor snapshot phases to watch.
	waitPhases := []backupdriverv1.SnapshotPhase{backupdriverv1.SnapshotPhaseUploading, backupdriverv1.SnapshotPhaseUploaded,
		backupdriverv1.SnapshotPhaseUploadFailed, backupdriverv1.SnapshotPhaseCanceling, backupdriverv1.SnapshotPhaseCanceled}

	// List of terminal phases. If we reach one of these phases, return.
	terminalPhases := []backupdriverv1.SnapshotPhase{backupdriverv1.SnapshotPhaseUploaded, backupdriverv1.SnapshotPhaseUploadFailed, backupdriverv1.SnapshotPhaseCanceled}

	svcSnapshot, err := svcClientSet.Snapshots(svcNamespace).Get(svcSnapshotName, metav1.GetOptions{})
	if err != nil {
		logger.WithError(err).Errorf("Did not find the supervisor snapshot %s", svcSnapshotName)
		return err
	}

	// Wait for the various supervisor snapshot status phase updates. If we reach a terminal state, return.
	// If not, update the snapshot with the new state and wait for remaining states.
	reachedTerminalState := false
	for !reachedTerminalState {
		svcSnapshot, err = WaitForPhases(ctx, svcClientSet, *svcSnapshot, waitPhases, svcNamespace, logger)
		if err != nil {
			logger.WithError(err).Errorf("Failed to wait for updated supervisor snapshot %s", svcSnapshotName)
			return err
		}
		err = updateSnapshotStatusPhase(snapshot, svcSnapshot.Status.Phase, clientSet)
		if err != nil {
			logger.WithError(err).Errorf("Failed to update snapshot %s/%s to phase %s", snapshot.Namespace, snapshot.Name, svcSnapshot.Status.Phase)
			return err
		}

		logger.Infof("Snapshot %s/%s status phase updated to %s", snapshot.Namespace, snapshot.Name, snapshot.Status.Phase)

		for _, p := range terminalPhases {
			if p == snapshot.Status.Phase {
				// we reached a terminal phase, return
				return nil
			}
		}

		// Not yet reached a terminal phase. Remove the new phase from the wait phases and retry
		for i, p := range waitPhases {
			if p == snapshot.Status.Phase {
				// we found the wait phase to remove.
				waitPhases = append(waitPhases[:i], waitPhases[i+1:]...)
				break
			}
		}
	}
	return nil
}

// Update the snapshot status phase
func updateSnapshotStatusPhase(
	snapshot *backupdriverv1.Snapshot,
	newPhase backupdriverv1.SnapshotPhase,
	clientSet *v1.BackupdriverV1Client) error {

	if snapshot.Status.Phase == newPhase {
		return nil
	}

	snapshotClone := snapshot.DeepCopy()
	snapshotClone.Status.Phase = newPhase
	_, err := clientSet.Snapshots(snapshotClone.Namespace).UpdateStatus(snapshotClone)

	return err
}
