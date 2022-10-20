package matcher

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	virtv1 "kubevirt.io/api/core/v1"
)

var _ = Describe("Field", func() {
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

	DescribeTable("should", func(vm interface{}, fields []string, matcher types.GomegaMatcher, expected bool, failMsg, negatedFailMsg string) {
		match, err := Field(matcher, fields...).Match(vm)
		Expect(err).ToNot(HaveOccurred())
		Expect(match).To(Equal(expected))
		Expect(Field(matcher, fields...).FailureMessage(vm)).To(Equal(failMsg))
		Expect(Field(matcher, fields...).NegatedFailureMessage(vm)).To(Equal(negatedFailMsg))
	},
		Entry("report a created vm as created", createdVM, []string{"Status", "Created"}, BeTrue(), true,
			"Expected\n    <bool>: true\nto be true",
			"Expected\n    <bool>: true\nnot to be true"),
		Entry("report a not created vm as not created", notCreatedVM, []string{"Status", "Created"}, BeFalse(), true,
			"Expected\n    <bool>: false\nto be false",
			"Expected\n    <bool>: false\nnot to be false"),
		Entry("cope with a nil vm", nil, []string{"Status", "Created"}, BeFalse(), false,
			"Expected\n    <nil>: nil\nto be false",
			"Expected\n    <nil>: nil\nnot to be false"),
		Entry("cope with an object pointing to nil", toNilPointer, []string{"Status", "Created"}, BeFalse(), false,
			"Expected\n    <nil>: nil\nto be false",
			"Expected\n    <nil>: nil\nnot to be false"),
	)

	DescribeTable("should cope with", func(vm interface{}, fields []string, matcher types.GomegaMatcher, errMsg string) {
		match, err := Field(matcher, fields...).Match(vm)
		Expect(match).To(BeFalse())
		Expect(err).To(MatchError(errMsg))
	},
		Entry("empty fields", createdVM, []string{}, BeTrue(), "Passed in fields may not be empty"),
		Entry("non-existent field", createdVM, []string{"Status", "MadeThisUp"}, BeTrue(), "Field 'Status.MadeThisUp' was not found"),
		Entry("last field not a struct", createdVM, []string{"Status", "Created", "MadeThisUp"}, BeTrue(), "Status.Created.MadeThisUp accessor error: true is of the type bool, expected reflect.Struct"),
	)
})
