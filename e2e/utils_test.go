//nolint:all
package e2e_test

import (
	"context"
	"fmt"
	"reflect"
	"time"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultTimeoutInterval = 20 * time.Second
	defaultPollInterval    = time.Second
	e2eLabel               = "argo.addons.projectcapsule.dev/e2e"
	suiteLabel             = "e2e.argo.addons.projectcapsule.dev/suite"
)

func e2eConfigName() string {
	return "default"
}

// Returns labels to identify e2e resources.
func e2eLabels(suite string) (labels map[string]string) {
	labels = make(map[string]string)
	labels["cortex.projectcapsule.dev/e2e"] = "true"

	if suite != "" {
		labels["cortex.projectcapsule.dev/suite"] = suite
	}

	return
}

// Returns a label selector to filter e2e resources.
func e2eSelector(suite string) labels.Selector {
	return labels.SelectorFromSet(e2eLabels(suite))
}

func CleanTenants(selector labels.Selector) error {
	res := &capsulev1beta2.TenantList{}

	listOptions := client.ListOptions{
		LabelSelector: selector,
	}

	// List the resources based on the provided label selector
	if err := k8sClient.List(context.TODO(), res, &listOptions); err != nil {
		return fmt.Errorf("failed to list tenants: %w", err)
	}

	for _, app := range res.Items {
		if err := k8sClient.Delete(context.TODO(), &app); err != nil {
			return fmt.Errorf("failed to delete tenant %s: %w", app.GetName(), err)
		}
	}

	return nil
}

func DeepCompare(expected, actual interface{}) (bool, string) {
	expVal := reflect.ValueOf(expected)
	actVal := reflect.ValueOf(actual)

	// If the kinds differ, they are not equal.
	if expVal.Kind() != actVal.Kind() {
		return false, fmt.Sprintf("kind mismatch: %v vs %v", expVal.Kind(), actVal.Kind())
	}

	switch expVal.Kind() {
	case reflect.Slice, reflect.Array:
		// Convert slices to []interface{} for ElementsMatch.
		expSlice := make([]interface{}, expVal.Len())
		actSlice := make([]interface{}, actVal.Len())
		for i := 0; i < expVal.Len(); i++ {
			expSlice[i] = expVal.Index(i).Interface()
		}
		for i := 0; i < actVal.Len(); i++ {
			actSlice[i] = actVal.Index(i).Interface()
		}
		// Use a dummy tester to capture error messages.
		dummy := &dummyT{}
		if !assert.ElementsMatch(dummy, expSlice, actSlice) {
			return false, fmt.Sprintf("slice mismatch: %v", dummy.errors)
		}
		return true, ""
	case reflect.Struct:
		// Iterate over fields and compare recursively.
		for i := 0; i < expVal.NumField(); i++ {
			fieldName := expVal.Type().Field(i).Name
			ok, msg := DeepCompare(expVal.Field(i).Interface(), actVal.Field(i).Interface())
			if !ok {
				return false, fmt.Sprintf("field %s mismatch: %s", fieldName, msg)
			}
		}
		return true, ""
	default:
		// Fallback to reflect.DeepEqual for other types.
		if !reflect.DeepEqual(expected, actual) {
			return false, fmt.Sprintf("expected %v but got %v", expected, actual)
		}
		return true, ""
	}
}

// dummyT implements a minimal TestingT for testify.
type dummyT struct {
	errors []string
}

func (d *dummyT) Errorf(format string, args ...interface{}) {
	d.errors = append(d.errors, fmt.Sprintf(format, args...))
}
