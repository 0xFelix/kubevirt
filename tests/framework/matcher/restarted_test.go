package matcher

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	virtv1 "kubevirt.io/api/core/v1"
)

var _ = Describe("Restarted", func() {

	var toNilPointer *virtv1.VirtualMachineInstance = nil

	var oldUID types.UID = "22e50082-3be5-4143-a1c2-ed75a446f7bd"

	var restartedVMI = &virtv1.VirtualMachineInstance{
		ObjectMeta: v1.ObjectMeta{
			UID: "acd691d3-e1df-4ee8-9400-e2f7e3c3f932",
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

	DescribeTable("should", func(vm interface{}, match bool) {
		restarted, err := BeRestarted(oldUID).Match(vm)
		Expect(err).ToNot(HaveOccurred())
		Expect(restarted).To(Equal(match))
		Expect(BeRestarted(oldUID).FailureMessage(vm)).ToNot(BeEmpty())
		Expect(BeRestarted(oldUID).NegatedFailureMessage(vm)).ToNot(BeEmpty())
	},
		Entry("report a restarted vmi as ready", restartedVMI, true),
		Entry("report a vmi with old UID as not restarted", vmiWithOldUID, false),
		Entry("report a vmi with phase Scheduling as not restarted", vmiWithPhaseScheduling, false),
		Entry("cope with a nil vmi", nil, false),
		Entry("cope with an object pointing to nil", toNilPointer, false),
	)
})
