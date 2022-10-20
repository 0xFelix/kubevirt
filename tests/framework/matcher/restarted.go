package matcher

import (
	"fmt"

	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "kubevirt.io/api/core/v1"

	"kubevirt.io/kubevirt/tests/framework/matcher/helper"
)

func BeRestarted(oldUID k8stypes.UID) types.GomegaMatcher {
	return restartedMatcher{
		oldUID: oldUID,
	}
}

type restartedMatcher struct {
	oldUID k8stypes.UID
}

func (r restartedMatcher) Match(actual interface{}) (success bool, err error) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return false, nil
	}
	uid, _, err := unstructured.NestedString(u.Object, "metadata", "uid")
	if err != nil {
		return false, nil
	}
	phase, _, err := unstructured.NestedString(u.Object, "status", "phase")
	if err != nil {
		return false, nil
	}
	return uid != string(r.oldUID) && phase == string(v1.Running), nil
}

func (r restartedMatcher) FailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected VMI to be restarted, but got '%v'", u)
}

func (r restartedMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	u, err := helper.ToUnstructured(actual)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("Expected VMI not to be restarted, but got '%v'", u)
}
