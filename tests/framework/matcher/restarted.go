package matcher

import (
	"errors"
	"fmt"

	"github.com/onsi/gomega/types"
	k8stypes "k8s.io/apimachinery/pkg/types"

	v1 "kubevirt.io/api/core/v1"
)

const objectNoVmi = "Object to match is no VMI"

func BeRestarted(oldUID k8stypes.UID) types.GomegaMatcher {
	return restartedMatcher{
		oldUID: oldUID,
	}
}

type restartedMatcher struct {
	oldUID k8stypes.UID
}

func (r restartedMatcher) Match(actual interface{}) (bool, error) {
	if actual == nil {
		return false, nil
	}

	vmi, ok := actual.(*v1.VirtualMachineInstance)
	if !ok {
		return false, errors.New(objectNoVmi)
	}

	if vmi == nil {
		return false, nil
	}

	return vmi.UID != r.oldUID && vmi.Status.Phase == v1.Running, nil
}

func (r restartedMatcher) FailureMessage(actual interface{}) string {
	return failMessage(actual, false)
}

func (r restartedMatcher) NegatedFailureMessage(actual interface{}) string {
	return failMessage(actual, true)
}

func failMessage(actual interface{}, negate bool) string {
	prefix := "Expected VMI"
	if negate {
		prefix = "Expected VMI not"
	}

	if actual == nil {
		return fmt.Sprintf("%s to be restarted, but got nil object", prefix)
	}

	vmi, ok := actual.(*v1.VirtualMachineInstance)
	if !ok {
		return fmt.Sprintf("%s to be restarted, but object to match is no VMI", prefix)
	}

	if vmi == nil {
		return fmt.Sprintf("%s to be restarted, but got nil object", prefix)
	}

	return fmt.Sprintf("%s to be restarted, but got uid '%s' and phase '%s'", prefix, vmi.UID, vmi.Status.Phase)
}
