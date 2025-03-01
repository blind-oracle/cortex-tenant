package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/cortex-tenant/internal/metrics"
	"github.com/projectcapsule/cortex-tenant/internal/stores"
)

// CapsuleArgocdReconciler reconciles a CapsuleArgocd object.
type TenantController struct {
	client.Client
	Metrics *metrics.Recorder
	Scheme  *runtime.Scheme
	Store   *stores.TenantStore
	Log     logr.Logger
}

func (r *TenantController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capsulev1beta2.Tenant{}).
		Complete(r)
}

func (r *TenantController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	origin := &capsulev1beta2.Tenant{}
	if err := r.Get(ctx, req.NamespacedName, origin); err != nil {
		r.lifecycle(&capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:      req.Name,
				Namespace: req.Namespace,
			},
		})

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	r.Store.Update(origin)

	return ctrl.Result{}, nil
}

// First execttion of the controller to load the settings (without manager cache).
func (r *TenantController) Init(ctx context.Context, client client.Client) (err error) {
	tnts := &capsulev1beta2.TenantList{}

	if err := client.List(ctx, tnts); err != nil {
		return fmt.Errorf("could not load tenants: %w", err)
	}

	for _, tnt := range tnts.Items {
		r.Store.Update(&tnt)
	}

	return
}

func (r *TenantController) lifecycle(tenant *capsulev1beta2.Tenant) {
	r.Store.Delete(&capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenant.Name,
			Namespace: tenant.Namespace,
		},
	})

	r.Metrics.DeleteMetricsForTenant(tenant)
}
