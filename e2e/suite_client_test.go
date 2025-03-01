//nolint:all
package e2e_test

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type e2eClient struct {
	client.Client
}

func (e *e2eClient) sleep() {
	time.Sleep(250 * time.Millisecond)
}

func (e *e2eClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	defer e.sleep()

	return e.Client.Get(ctx, key, obj, opts...)
}

func (e *e2eClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	defer e.sleep()

	return e.Client.List(ctx, list, opts...)
}

func (e *e2eClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	defer e.sleep()
	obj.SetResourceVersion("")

	return e.Client.Create(ctx, obj, opts...)
}

func (e *e2eClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	defer e.sleep()

	return e.Client.Delete(ctx, obj, opts...)
}

func (e *e2eClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	defer e.sleep()

	return e.Client.Update(ctx, obj, opts...)
}

func (e *e2eClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	defer e.sleep()

	return e.Client.Patch(ctx, obj, patch, opts...)
}

func (e *e2eClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	defer e.sleep()

	return e.Client.DeleteAllOf(ctx, obj, opts...)
}
