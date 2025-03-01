package stores_test

import (
	"sync"
	"testing"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/cortex-tenant/internal/stores"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/gomega"
)

// TestTenantStore_Basic verifies that updating, retrieving, and deleting tenants works as expected.
func TestTenantStore_Basic(t *testing.T) {
	RegisterTestingT(t)

	store := stores.NewTenantStore()

	tenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant1",
		},
		Status: capsulev1beta2.TenantStatus{
			Namespaces: []string{"ns1", "ns2"},
		},
	}

	// Update the store with tenant1 for ns1 and ns2.
	store.Update(tenant)
	Expect(store.GetTenant("ns1")).To(Equal("tenant1"))
	Expect(store.GetTenant("ns2")).To(Equal("tenant1"))

	// Now update tenant: remove ns1 and add ns3.
	tenant.Status.Namespaces = []string{"ns2", "ns3"}
	store.Update(tenant)
	Expect(store.GetTenant("ns1")).To(Equal(""))
	Expect(store.GetTenant("ns2")).To(Equal("tenant1"))
	Expect(store.GetTenant("ns3")).To(Equal("tenant1"))

	// Delete tenant; ns2 and ns3 should be removed.
	store.Delete(tenant)
	Expect(store.GetTenant("ns2")).To(Equal(""))
	Expect(store.GetTenant("ns3")).To(Equal(""))
}

// TestTenantStore_Concurrent verifies that concurrent reads work safely.
func TestTenantStore_Concurrent(t *testing.T) {
	RegisterTestingT(t)

	store := stores.NewTenantStore()
	tenant := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant1",
		},
		Status: capsulev1beta2.TenantStatus{
			Namespaces: []string{"ns1", "ns2", "ns3"},
		},
	}
	store.Update(tenant)

	var wg sync.WaitGroup
	numGoroutines := 50
	iterations := 1000

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Concurrently read from the store.
				_ = store.GetTenant("ns1")
				_ = store.GetTenant("ns2")
				_ = store.GetTenant("ns3")
			}
		}()
	}

	wg.Wait()
}
