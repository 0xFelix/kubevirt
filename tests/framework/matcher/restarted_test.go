package matcher

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	virtv1 "kubevirt.io/api/core/v1"
)

var _ = Describe("Restarted", func() {
	var toNilPointer *virtv1.VirtualMachineInstance = nil

	var newUID types.UID = "acd691d3-e1df-4ee8-9400-e2f7e3c3f932"
	var oldUID types.UID = "22e50082-3be5-4143-a1c2-ed75a446f7bd"

	var restartedVMI = &virtv1.VirtualMachineInstance{
		ObjectMeta: v1.ObjectMeta{
			UID: newUID,
		},
		Status: virtv1.VirtualMachineInstanceStatus{
			Phase: virtv1.Running,
		},
	}

	var vmiWithOldUID = &virtv1.VirtualMachineInstance{
		ObjectMeta: v1.ObjectMeta{
			UID: oldUID,
		},
		Status: virtv1.VirtualMachineInstanceStatus{
			Phase: virtv1.Running,
		},
	}

	var vmiWithPhaseScheduling = &virtv1.VirtualMachineInstance{
		ObjectMeta: v1.ObjectMeta{
			UID: oldUID,
		},
		Status: virtv1.VirtualMachineInstanceStatus{
			Phase: virtv1.Scheduling,
		},
	}

	DescribeTable("should", func(vmi interface{}, match bool, failMsg, negatedFailMsg string) {
		restarted, err := BeRestarted(oldUID).Match(vmi)
		Expect(err).ToNot(HaveOccurred())
		Expect(restarted).To(Equal(match))
		Expect(BeRestarted(oldUID).FailureMessage(vmi)).To(Equal(failMsg))
		Expect(BeRestarted(oldUID).NegatedFailureMessage(vmi)).To(Equal(negatedFailMsg))
	},
		Entry("report a restarted vmi as ready", restartedVMI, true,
			fmt.Sprintf("Expected VMI to be restarted, but got uid '%s' and phase '%s'", newUID, virtv1.Running),
			fmt.Sprintf("Expected VMI not to be restarted, but got uid '%s' and phase '%s'", newUID, virtv1.Running)),
		Entry("report a vmi with old UID as not restarted", vmiWithOldUID, false,
			fmt.Sprintf("Expected VMI to be restarted, but got uid '%s' and phase '%s'", oldUID, virtv1.Running),
			fmt.Sprintf("Expected VMI not to be restarted, but got uid '%s' and phase '%s'", oldUID, virtv1.Running)),
		Entry("report a vmi with phase Scheduling as not restarted", vmiWithPhaseScheduling, false,
			fmt.Sprintf("Expected VMI to be restarted, but got uid '%s' and phase '%s'", oldUID, virtv1.Scheduling),
			fmt.Sprintf("Expected VMI not to be restarted, but got uid '%s' and phase '%s'", oldUID, virtv1.Scheduling)),
		Entry("cope with a nil vmi", nil, false,
			"Expected VMI to be restarted, but got nil object",
			"Expected VMI not to be restarted, but got nil object"),
		Entry("cope with an object pointing to nil", toNilPointer, false,
			"Expected VMI to be restarted, but got nil object",
			"Expected VMI not to be restarted, but got nil object"),
	)

	It("should cope with an object that is no vmi", func() {
		obj := &virtv1.VirtualMachine{}
		restarted, err := BeRestarted(oldUID).Match(obj)
		Expect(err).To(MatchError("Object to match is no VMI"))
		Expect(restarted).To(BeFalse())
		Expect(BeRestarted(oldUID).FailureMessage(obj)).To(Equal("Expected VMI to be restarted, but object to match is no VMI"))
		Expect(BeRestarted(oldUID).NegatedFailureMessage(obj)).To(Equal("Expected VMI not to be restarted, but object to match is no VMI"))
	})
})
