package matcher

import (
	"fmt"

	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"kubevirt.io/kubevirt/tests/framework/matcher/helper"
)

func BeReady() types.GomegaMatcher {
	return readyMatcher{}
}

type readyMatcher struct {
}

func (r readyMatcher) Match(actual interface{}) (success bool, err error) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return false, nil
	}
	ready, _, err := unstructured.NestedBool(u.Object, "status", "ready")
	return ready, err
}

func (r readyMatcher) FailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected status ready to be true, but got '%v'", u)
}

func (r readyMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected status ready to be false, but got '%v'", u)
}
