package matcher

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	virtv1 "kubevirt.io/api/core/v1"
)

var _ = Describe("StateChangeRequests", func() {

	var toNilPointer *virtv1.VirtualMachine = nil

	var vmWithStateChangeRequests = &virtv1.VirtualMachine{
		Status: virtv1.VirtualMachineStatus{
			StateChangeRequests: []virtv1.VirtualMachineStateChangeRequest{
				{
					Action: virtv1.StopRequest,
				},
			},
		},
	}

	var vmWithEmptyStateChangeRequests = &virtv1.VirtualMachine{
		Status: virtv1.VirtualMachineStatus{
			StateChangeRequests: []virtv1.VirtualMachineStateChangeRequest{},
		},
	}

	var vmWithoutStateChangeRequests = &virtv1.VirtualMachine{
		Status: virtv1.VirtualMachineStatus{
			StateChangeRequests: nil,
		},
	}

	DescribeTable("should", func(vm interface{}, match bool) {
		hasStateChangeRequests, err := HaveStateChangeRequests().Match(vm)
		Expect(err).ToNot(HaveOccurred())
		Expect(hasStateChangeRequests).To(Equal(match))
		Expect(HaveStateChangeRequests().FailureMessage(vm)).ToNot(BeEmpty())
		Expect(HaveStateChangeRequests().NegatedFailureMessage(vm)).ToNot(BeEmpty())
	},
		Entry("report a vm with stateChangeRequests to have stateChangeRequests", vmWithStateChangeRequests, true),
		Entry("report a vm with empty stateChangeRequests to have no stateChangeRequests", vmWithEmptyStateChangeRequests, false),
		Entry("report a vm without stateChangeRequests to have no stateChangeRequests", vmWithoutStateChangeRequests, false),
		Entry("cope with a nil vm", nil, false),
		Entry("cope with an object pointing to nil", toNilPointer, false),
	)
})
