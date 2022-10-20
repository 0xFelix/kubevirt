package matcher

import (
	"fmt"

	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"kubevirt.io/kubevirt/tests/framework/matcher/helper"
)

func BeCreated() types.GomegaMatcher {
	return createdMatcher{}
}

type createdMatcher struct {
}

func (c createdMatcher) Match(actual interface{}) (success bool, err error) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return false, nil
	}
	created, _, err := unstructured.NestedBool(u.Object, "status", "created")
	return created, err
}

func (c createdMatcher) FailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected status created to be true, but got '%v'", u)
}

func (c createdMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected status created to be false, but got '%v'", u)
}
