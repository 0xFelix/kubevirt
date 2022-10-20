package matcher

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	virtv1 "kubevirt.io/api/core/v1"
)

var _ = Describe("Ready", func() {

	var toNilPointer *virtv1.VirtualMachine = nil

	var readyVM = &virtv1.VirtualMachine{
		Status: virtv1.VirtualMachineStatus{
			Ready: true,
		},
	}

	var notReadyVM = &virtv1.VirtualMachine{
		Status: virtv1.VirtualMachineStatus{
			Ready: false,
		},
	}

	DescribeTable("should", func(vm interface{}, match bool) {
		ready, err := BeReady().Match(vm)
		Expect(err).ToNot(HaveOccurred())
		Expect(ready).To(Equal(match))
		Expect(BeReady().FailureMessage(vm)).ToNot(BeEmpty())
		Expect(BeReady().NegatedFailureMessage(vm)).ToNot(BeEmpty())
	},
		Entry("report a ready vm as ready", readyVM, true),
		Entry("report a not ready vm as not ready", notReadyVM, false),
		Entry("cope with a nil vm", nil, false),
		Entry("cope with an object pointing to nil", toNilPointer, false),
	)
})
