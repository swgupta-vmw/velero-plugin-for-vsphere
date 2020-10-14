/*
Copyright the Velero contributors.

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

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1alpha1 "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/apis/backupdriver/v1alpha1"
	scheme "github.com/vmware-tanzu/velero-plugin-for-vsphere/pkg/generated/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// CloneFromSnapshotsGetter has a method to return a CloneFromSnapshotInterface.
// A group's client should implement this interface.
type CloneFromSnapshotsGetter interface {
	CloneFromSnapshots(namespace string) CloneFromSnapshotInterface
}

// CloneFromSnapshotInterface has methods to work with CloneFromSnapshot resources.
type CloneFromSnapshotInterface interface {
	Create(ctx context.Context, cloneFromSnapshot *v1alpha1.CloneFromSnapshot, opts v1.CreateOptions) (*v1alpha1.CloneFromSnapshot, error)
	Update(ctx context.Context, cloneFromSnapshot *v1alpha1.CloneFromSnapshot, opts v1.UpdateOptions) (*v1alpha1.CloneFromSnapshot, error)
	UpdateStatus(ctx context.Context, cloneFromSnapshot *v1alpha1.CloneFromSnapshot, opts v1.UpdateOptions) (*v1alpha1.CloneFromSnapshot, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.CloneFromSnapshot, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.CloneFromSnapshotList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.CloneFromSnapshot, err error)
	CloneFromSnapshotExpansion
}

// cloneFromSnapshots implements CloneFromSnapshotInterface
type cloneFromSnapshots struct {
	client rest.Interface
	ns     string
}

// newCloneFromSnapshots returns a CloneFromSnapshots
func newCloneFromSnapshots(c *BackupdriverV1alpha1Client, namespace string) *cloneFromSnapshots {
	return &cloneFromSnapshots{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the cloneFromSnapshot, and returns the corresponding cloneFromSnapshot object, and an error if there is any.
func (c *cloneFromSnapshots) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.CloneFromSnapshot, err error) {
	result = &v1alpha1.CloneFromSnapshot{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of CloneFromSnapshots that match those selectors.
func (c *cloneFromSnapshots) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.CloneFromSnapshotList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.CloneFromSnapshotList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested cloneFromSnapshots.
func (c *cloneFromSnapshots) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a cloneFromSnapshot and creates it.  Returns the server's representation of the cloneFromSnapshot, and an error, if there is any.
func (c *cloneFromSnapshots) Create(ctx context.Context, cloneFromSnapshot *v1alpha1.CloneFromSnapshot, opts v1.CreateOptions) (result *v1alpha1.CloneFromSnapshot, err error) {
	result = &v1alpha1.CloneFromSnapshot{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(cloneFromSnapshot).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a cloneFromSnapshot and updates it. Returns the server's representation of the cloneFromSnapshot, and an error, if there is any.
func (c *cloneFromSnapshots) Update(ctx context.Context, cloneFromSnapshot *v1alpha1.CloneFromSnapshot, opts v1.UpdateOptions) (result *v1alpha1.CloneFromSnapshot, err error) {
	result = &v1alpha1.CloneFromSnapshot{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		Name(cloneFromSnapshot.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(cloneFromSnapshot).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *cloneFromSnapshots) UpdateStatus(ctx context.Context, cloneFromSnapshot *v1alpha1.CloneFromSnapshot, opts v1.UpdateOptions) (result *v1alpha1.CloneFromSnapshot, err error) {
	result = &v1alpha1.CloneFromSnapshot{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		Name(cloneFromSnapshot.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(cloneFromSnapshot).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the cloneFromSnapshot and deletes it. Returns an error if one occurs.
func (c *cloneFromSnapshots) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *cloneFromSnapshots) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched cloneFromSnapshot.
func (c *cloneFromSnapshots) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.CloneFromSnapshot, err error) {
	result = &v1alpha1.CloneFromSnapshot{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("clonefromsnapshots").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}