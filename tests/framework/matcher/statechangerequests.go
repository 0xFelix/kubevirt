package matcher

import (
	"fmt"

	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"kubevirt.io/kubevirt/tests/framework/matcher/helper"
)

func HaveStateChangeRequests() types.GomegaMatcher {
	return stateChangeRequestsMatcher{}
}

type stateChangeRequestsMatcher struct {
}

func (r stateChangeRequestsMatcher) Match(actual interface{}) (success bool, err error) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return false, nil
	}
	stateChangeRequests, found, err := unstructured.NestedSlice(u.Object, "status", "stateChangeRequests")
	return found && len(stateChangeRequests) > 0, err
}

func (r stateChangeRequestsMatcher) FailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected VMI to have stateChangeRequests, but got '%v'", u)
}

func (r stateChangeRequestsMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected VMI not to have stateChangeRequests, but got '%v'", u)
}
