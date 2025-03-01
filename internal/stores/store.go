package stores

import (
	"sync"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type TenantStore struct {
	sync.RWMutex
	tenants map[string]string
}

func NewTenantStore() *TenantStore {
	return &TenantStore{
		tenants: make(map[string]string),
	}
}

func (s *TenantStore) GetTenant(namespace string) string {
	s.RLock()
	defer s.RUnlock()

	return s.tenants[namespace]
}

func (s *TenantStore) Update(tenant *capsulev1beta2.Tenant) {
	s.Lock()
	defer s.Unlock()

	currentNamespaces := make(map[string]struct{}, len(tenant.Status.Namespaces))
	for _, ns := range tenant.Status.Namespaces {
		currentNamespaces[ns] = struct{}{}
	}

	for ns, t := range s.tenants {
		if t == tenant.Name {
			// If ns is not in the updated namespace list, delete it
			if _, exists := currentNamespaces[ns]; !exists {
				delete(s.tenants, ns)
			}
		}
	}

	for _, ns := range tenant.Status.Namespaces {
		s.tenants[ns] = tenant.Name
	}
}

func (s *TenantStore) Delete(tenant *capsulev1beta2.Tenant) {
	s.Lock()
	defer s.Unlock()

	for ns, t := range s.tenants {
		if t == tenant.Name {
			delete(s.tenants, ns)
		}
	}
}
