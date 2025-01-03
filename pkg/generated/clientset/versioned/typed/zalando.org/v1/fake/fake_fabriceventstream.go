/*
Copyright 2025 Compose, Zalando SE

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	zalandoorgv1 "github.com/zalando/postgres-operator/pkg/apis/zalando.org/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeFabricEventStreams implements FabricEventStreamInterface
type FakeFabricEventStreams struct {
	Fake *FakeZalandoV1
	ns   string
}

var fabriceventstreamsResource = schema.GroupVersionResource{Group: "zalando.org", Version: "v1", Resource: "fabriceventstreams"}

var fabriceventstreamsKind = schema.GroupVersionKind{Group: "zalando.org", Version: "v1", Kind: "FabricEventStream"}

// Get takes name of the fabricEventStream, and returns the corresponding fabricEventStream object, and an error if there is any.
func (c *FakeFabricEventStreams) Get(ctx context.Context, name string, options v1.GetOptions) (result *zalandoorgv1.FabricEventStream, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(fabriceventstreamsResource, c.ns, name), &zalandoorgv1.FabricEventStream{})

	if obj == nil {
		return nil, err
	}
	return obj.(*zalandoorgv1.FabricEventStream), err
}

// List takes label and field selectors, and returns the list of FabricEventStreams that match those selectors.
func (c *FakeFabricEventStreams) List(ctx context.Context, opts v1.ListOptions) (result *zalandoorgv1.FabricEventStreamList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(fabriceventstreamsResource, fabriceventstreamsKind, c.ns, opts), &zalandoorgv1.FabricEventStreamList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &zalandoorgv1.FabricEventStreamList{ListMeta: obj.(*zalandoorgv1.FabricEventStreamList).ListMeta}
	for _, item := range obj.(*zalandoorgv1.FabricEventStreamList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested fabricEventStreams.
func (c *FakeFabricEventStreams) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(fabriceventstreamsResource, c.ns, opts))

}

// Create takes the representation of a fabricEventStream and creates it.  Returns the server's representation of the fabricEventStream, and an error, if there is any.
func (c *FakeFabricEventStreams) Create(ctx context.Context, fabricEventStream *zalandoorgv1.FabricEventStream, opts v1.CreateOptions) (result *zalandoorgv1.FabricEventStream, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(fabriceventstreamsResource, c.ns, fabricEventStream), &zalandoorgv1.FabricEventStream{})

	if obj == nil {
		return nil, err
	}
	return obj.(*zalandoorgv1.FabricEventStream), err
}

// Update takes the representation of a fabricEventStream and updates it. Returns the server's representation of the fabricEventStream, and an error, if there is any.
func (c *FakeFabricEventStreams) Update(ctx context.Context, fabricEventStream *zalandoorgv1.FabricEventStream, opts v1.UpdateOptions) (result *zalandoorgv1.FabricEventStream, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(fabriceventstreamsResource, c.ns, fabricEventStream), &zalandoorgv1.FabricEventStream{})

	if obj == nil {
		return nil, err
	}
	return obj.(*zalandoorgv1.FabricEventStream), err
}

// Delete takes name of the fabricEventStream and deletes it. Returns an error if one occurs.
func (c *FakeFabricEventStreams) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(fabriceventstreamsResource, c.ns, name, opts), &zalandoorgv1.FabricEventStream{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeFabricEventStreams) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(fabriceventstreamsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &zalandoorgv1.FabricEventStreamList{})
	return err
}

// Patch applies the patch and returns the patched fabricEventStream.
func (c *FakeFabricEventStreams) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *zalandoorgv1.FabricEventStream, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(fabriceventstreamsResource, c.ns, name, pt, data, subresources...), &zalandoorgv1.FabricEventStream{})

	if obj == nil {
		return nil, err
	}
	return obj.(*zalandoorgv1.FabricEventStream), err
}
