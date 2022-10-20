package matcher

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	virtv1 "kubevirt.io/api/core/v1"
)

var _ = Describe("Created", func() {

	var toNilPointer *virtv1.VirtualMachine = nil

	var createdVM = &virtv1.VirtualMachine{
		Status: virtv1.VirtualMachineStatus{
			Created: true,
		},
	}

	var notCreatedVM = &virtv1.VirtualMachine{
		Status: virtv1.VirtualMachineStatus{
			Created: false,
		},
	}

	DescribeTable("should", func(vm interface{}, match bool) {
		ready, err := BeCreated().Match(vm)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(Equal(match))
		Expect(BeCreated().FailureMessage(vm)).ToNot(BeEmpty())
		Expect(BeCreated().NegatedFailureMessage(vm)).ToNot(BeEmpty())
	},
		Entry("report a created vm as created", createdVM, true),
		Entry("report a not created vm as not created", notCreatedVM, false),
		Entry("cope with a nil vm", nil, false),
		Entry("cope with an object pointing to nil", toNilPointer, false),
	)
})
