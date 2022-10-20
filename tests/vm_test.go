/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package tests_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	expect "github.com/google/goexpect"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pborman/uuid"
	corev1 "k8s.io/api/core/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/utils/pointer"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	cdiv1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"

	"kubevirt.io/kubevirt/pkg/controller"
	virtctl "kubevirt.io/kubevirt/pkg/virtctl/vm"
	"kubevirt.io/kubevirt/tests"
	"kubevirt.io/kubevirt/tests/clientcmd"
	"kubevirt.io/kubevirt/tests/console"
	cd "kubevirt.io/kubevirt/tests/containerdisk"
	. "kubevirt.io/kubevirt/tests/framework/matcher"
	"kubevirt.io/kubevirt/tests/libnode"
	"kubevirt.io/kubevirt/tests/libstorage"
	"kubevirt.io/kubevirt/tests/libvmi"
	"kubevirt.io/kubevirt/tests/util"
	"kubevirt.io/kubevirt/tools/vms-generator/utils"
)

var _ = Describe("[rfe_id:1177][crit:medium][vendor:cnv-qe@redhat.com][level:component][sig-compute]VirtualMachine", func() {
	var err error
	var virtClient kubecli.KubevirtClient

	runStrategyAlways := v1.RunStrategyAlways
	runStrategyHalted := v1.RunStrategyHalted
	runStrategyManual := v1.RunStrategyManual

	createVM := func(template *v1.VirtualMachineInstance, running bool) *v1.VirtualMachine {
		By("Creating VirtualMachine")
		newVM := tests.NewRandomVirtualMachine(template, running)
		newVM, err := virtClient.VirtualMachine(util.NamespaceTestDefault).Create(newVM)
		Expect(err).ToNot(HaveOccurred())
		return newVM
	}

	BeforeEach(func() {
		virtClient, err = kubecli.GetKubevirtClient()
		util.PanicOnError(err)
	})

	Context("An invalid VirtualMachine given", func() {
		It("[test_id:1518]should be rejected on POST", func() {
			newVM := tests.NewRandomVirtualMachine(libvmi.NewCirros(), false)
			// because we're marshaling this ourselves, we have to make sure
			// we're using the same version the virtClient is using.
			newVM.APIVersion = "kubevirt.io/" + v1.ApiStorageVersion

			jsonBytes, err := json.Marshal(newVM)
			Expect(err).ToNot(HaveOccurred())

			// change the name of a required field (like domain) so validation will fail
			jsonString := strings.Replace(string(jsonBytes), "domain", "not-a-domain", -1)

			result := virtClient.RestClient().Post().Resource("virtualmachines").Namespace(util.NamespaceTestDefault).Body([]byte(jsonString)).SetHeader("Content-Type", "application/json").Do(context.Background())
			// Verify validation failed.
			statusCode := 0
			result.StatusCode(&statusCode)
			Expect(statusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("[test_id:1519]should reject POST if validation webhoook deems the spec is invalid", func() {
			template := libvmi.NewCirros()
			// Add a disk that doesn't map to a volume.
			// This should get rejected which tells us the webhook validator is working.
			template.Spec.Domain.Devices.Disks = append(template.Spec.Domain.Devices.Disks, v1.Disk{
				Name: "testdisk",
			})
			newVM := tests.NewRandomVirtualMachine(template, false)

			result := virtClient.RestClient().Post().Resource("virtualmachines").Namespace(util.NamespaceTestDefault).Body(newVM).Do(context.Background())

			// Verify validation failed.
			statusCode := 0
			result.StatusCode(&statusCode)
			Expect(statusCode).To(Equal(http.StatusUnprocessableEntity))

			reviewResponse := &k8smetav1.Status{}
			body, _ := result.Raw()
			err = json.Unmarshal(body, reviewResponse)
			Expect(err).ToNot(HaveOccurred())

			Expect(reviewResponse.Details.Causes).To(HaveLen(1))
			Expect(reviewResponse.Details.Causes[0].Field).To(Equal("spec.template.spec.domain.devices.disks[2].name"))
		})
	})

	Context("[Serial]A mutated VirtualMachine given", func() {
		const testingMachineType = "pc-q35-2.7"

		BeforeEach(func() {
			kv := util.GetCurrentKv(virtClient)
			kubevirtConfiguration := kv.Spec.Configuration

			kubevirtConfiguration.MachineType = testingMachineType
			tests.UpdateKubeVirtConfigValueAndWait(kubevirtConfiguration)
		})

		It("[test_id:3312]should set the default MachineType when created without explicit value", func() {
			By("Creating VirtualMachine")
			template := libvmi.NewCirros()
			template.Spec.Domain.Machine = nil
			vm := createVM(template, false)
			Expect(vm.Spec.Template.Spec.Domain.Machine.Type).To(Equal(testingMachineType))
		})

		It("[test_id:3311]should keep the supplied MachineType when created", func() {
			By("Creating VirtualMachine")
			explicitMachineType := "pc-q35-3.0"
			template := libvmi.NewCirros()
			template.Spec.Domain.Machine = &v1.Machine{Type: explicitMachineType}
			vm := createVM(template, false)
			Expect(vm.Spec.Template.Spec.Domain.Machine.Type).To(Equal(explicitMachineType))
		})
	})

	Context("A valid VirtualMachine given", func() {
		type vmiBuilder func() (*v1.VirtualMachineInstance, *cdiv1.DataVolume)

		newVirtualMachineInstanceWithContainerDisk := func() (*v1.VirtualMachineInstance, *cdiv1.DataVolume) {
			return libvmi.NewCirros(), nil
		}

		newVirtualMachineInstanceWithFileDisk := func() (*v1.VirtualMachineInstance, *cdiv1.DataVolume) {
			return tests.NewRandomVirtualMachineInstanceWithFileDisk(cd.DataVolumeImportUrlForContainerDisk(cd.ContainerDiskAlpine), util.NamespaceTestDefault, corev1.ReadWriteOnce)
		}

		newVirtualMachineInstanceWithBlockDisk := func() (*v1.VirtualMachineInstance, *cdiv1.DataVolume) {
			return tests.NewRandomVirtualMachineInstanceWithBlockDisk(cd.DataVolumeImportUrlForContainerDisk(cd.ContainerDiskAlpine), util.NamespaceTestDefault, corev1.ReadWriteOnce)
		}

		createCirrosWithRunStrategy := func(runStrategy v1.VirtualMachineRunStrategy) *v1.VirtualMachine {
			newVM := tests.NewRandomVirtualMachine(libvmi.NewCirros(), false)
			newVM.Spec.Running = nil
			newVM.Spec.RunStrategy = &runStrategy
			newVM, err := virtClient.VirtualMachine(util.NamespaceTestDefault).Create(newVM)
			Expect(err).ToNot(HaveOccurred())
			return newVM
		}

		startVM := func(vm *v1.VirtualMachine) *v1.VirtualMachine {
			By("Waiting for VM to exist")
			Eventually(ThisVM(vm), 300).Should(Exist())

			By("Starting the VirtualMachine")
			err = tests.RetryWithMetadataIfModified(vm.ObjectMeta, func(meta k8smetav1.ObjectMeta) error {
				updatedVM, err := virtClient.VirtualMachine(meta.Namespace).Get(meta.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				updatedVM.Spec.Running = nil
				updatedVM.Spec.RunStrategy = &runStrategyAlways
				_, err = virtClient.VirtualMachine(meta.Namespace).Update(updatedVM)
				return err
			})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for VMI to be running")
			Eventually(ThisVMIWith(vm.Namespace, vm.Name), 300).Should(BeRunning())

			By("Waiting for VM to be ready")
			Eventually(ThisVM(vm), 360).Should(BeReady())

			vm, err = virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			return vm
		}

		stopVM := func(vm *v1.VirtualMachine) *v1.VirtualMachine {
			By("Stopping the VirtualMachine")
			err = tests.RetryWithMetadataIfModified(vm.ObjectMeta, func(meta k8smetav1.ObjectMeta) error {
				updatedVM, err := virtClient.VirtualMachine(meta.Namespace).Get(meta.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				updatedVM.Spec.Running = nil
				updatedVM.Spec.RunStrategy = &runStrategyHalted
				_, err = virtClient.VirtualMachine(meta.Namespace).Update(updatedVM)
				return err
			})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for VMI to not exist")
			Eventually(ThisVMIWith(vm.Namespace, vm.Name), 300).ShouldNot(Exist())

			By("Waiting for VM to not be ready")
			Eventually(ThisVM(vm), 300).ShouldNot(BeReady())

			vm, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			return vm
		}

		DescribeTable("cpu/memory in requests/limits should allow", func(cpu, request string) {
			const oldCpu = "222"
			const oldMemory = "2222222"

			vm := tests.NewRandomVirtualMachine(libvmi.NewCirros(), false)
			vm.Namespace = util.NamespaceTestDefault
			vm.APIVersion = "kubevirt.io/" + v1.ApiStorageVersion
			vm.Spec.Template.Spec.Domain.Resources.Limits = make(k8sv1.ResourceList)
			vm.Spec.Template.Spec.Domain.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(oldCpu)
			vm.Spec.Template.Spec.Domain.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(oldCpu)
			vm.Spec.Template.Spec.Domain.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(oldMemory)
			vm.Spec.Template.Spec.Domain.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(oldMemory)

			jsonBytes, err := json.Marshal(vm)
			Expect(err).NotTo(HaveOccurred())

			match := func(str string) string {
				return fmt.Sprintf("\"%s\"", str)
			}

			jsonString := strings.Replace(string(jsonBytes), match(oldCpu), cpu, -1)
			jsonString = strings.Replace(jsonString, match(oldMemory), request, -1)

			By("Verify VM can be created")
			result := virtClient.RestClient().Post().Resource("virtualmachines").Namespace(util.NamespaceTestDefault).Body([]byte(jsonString)).SetHeader("Content-Type", "application/json").Do(context.Background())
			statusCode := 0
			result.StatusCode(&statusCode)
			Expect(statusCode).To(Equal(http.StatusCreated))

			By("Verify VM will run")
			startVM(vm)
		},
			Entry("int type", "2", "2222222"),
			Entry("float type", "2.2", "2222222.2"),
		)

		It("[test_id:3161]should carry annotations to VMI", func() {
			annotations := map[string]string{
				"testannotation": "test",
			}

			vm := createVM(libvmi.NewCirros(), false)
			err = tests.RetryWithMetadataIfModified(vm.ObjectMeta, func(meta k8smetav1.ObjectMeta) error {
				vm, err = virtClient.VirtualMachine(meta.Namespace).Get(meta.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				vm.Spec.Template.ObjectMeta.Annotations = annotations
				vm, err = virtClient.VirtualMachine(meta.Namespace).Update(vm)
				return err
			})
			Expect(err).ToNot(HaveOccurred())

			startVM(vm)

			By("checking for annotations to be present")
			vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(vmi.Annotations).To(HaveKeyWithValue("testannotation", "test"))
		})

		It("[test_id:3162]should ignore kubernetes and kubevirt annotations to VMI", func() {
			annotations := map[string]string{
				"kubevirt.io/test":   "test",
				"kubernetes.io/test": "test",
			}

			vm := createVM(libvmi.NewCirros(), false)
			err = tests.RetryWithMetadataIfModified(vm.ObjectMeta, func(meta k8smetav1.ObjectMeta) error {
				vm, err = virtClient.VirtualMachine(meta.Namespace).Get(meta.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				vm.Annotations = annotations
				vm, err = virtClient.VirtualMachine(meta.Namespace).Update(vm)
				return err
			})
			Expect(err).ToNot(HaveOccurred())

			startVM(vm)

			By("checking for annotations to not be present")
			vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(vmi.Annotations).ShouldNot(HaveKey("kubevirt.io/test"), "kubevirt internal annotations should be ignored")
			Expect(vmi.Annotations).ShouldNot(HaveKey("kubernetes.io/test"), "kubernetes internal annotations should be ignored")
		})

		DescribeTable("[test_id:1520]should update VirtualMachine once VMIs are up", func(createTemplate vmiBuilder) {
			template, dv := createTemplate()
			defer libstorage.DeleteDataVolume(&dv)
			startVM(createVM(template, false))
		},
			Entry("with ContainerDisk", newVirtualMachineInstanceWithContainerDisk),
			Entry("[Serial][storage-req]with Filesystem Disk", newVirtualMachineInstanceWithFileDisk),
			Entry("[Serial][storage-req]with Block Disk", newVirtualMachineInstanceWithBlockDisk),
		)

		DescribeTable("[test_id:1521]should remove VirtualMachineInstance once the VM is marked for deletion", func(createTemplate vmiBuilder) {
			template, dv := createTemplate()
			defer libstorage.DeleteDataVolume(&dv)
			newVM := startVM(createVM(template, false))
			// Delete it
			Expect(virtClient.VirtualMachine(newVM.Namespace).Delete(newVM.Name, &k8smetav1.DeleteOptions{})).To(Succeed())
			// Wait until VMI is gone
			Eventually(ThisVMIWith(newVM.Namespace, newVM.Name), 300).ShouldNot(Exist())
		},
			Entry("with ContainerDisk", newVirtualMachineInstanceWithContainerDisk),
			Entry("[Serial][storage-req]with Filesystem Disk", newVirtualMachineInstanceWithFileDisk),
			Entry("[Serial][storage-req]with Block Disk", newVirtualMachineInstanceWithBlockDisk),
		)

		It("[test_id:1522]should remove owner references on the VirtualMachineInstance if it is orphan deleted", func() {
			newVM := startVM(createVM(libvmi.NewCirros(), false))

			By("Getting owner references")
			vmi, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(vmi.OwnerReferences).ToNot(BeEmpty())

			By("Deleting VM")
			orphanPolicy := k8smetav1.DeletePropagationOrphan
			err = virtClient.VirtualMachine(newVM.Namespace).Delete(newVM.Name, &k8smetav1.DeleteOptions{PropagationPolicy: &orphanPolicy})
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for VM to delete")
			Eventually(ThisVM(newVM), 300).ShouldNot(Exist())

			By("Verifying orphaned VMI still exists")
			vmi, err = virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(vmi.OwnerReferences).To(BeEmpty())
		})

		It("[test_id:1523]should recreate VirtualMachineInstance if it gets deleted", func() {
			newVM := startVM(createVM(libvmi.NewCirros(), false))

			currentVMI, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			err = virtClient.VirtualMachineInstance(newVM.Namespace).Delete(newVM.Name, &k8smetav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())

			Eventually(ThisVMI(currentVMI), 240).Should(BeRestarted(currentVMI.UID))
		})

		It("[test_id:1524]should recreate VirtualMachineInstance if the VirtualMachineInstance's pod gets deleted", func() {
			By("Start a new VM")
			newVM := startVM(createVM(libvmi.NewCirros(), false))
			firstVMI, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			// get the pod backing the VirtualMachineInstance
			By("Getting the pod backing the VirtualMachineInstance")
			pods, err := virtClient.CoreV1().Pods(newVM.Namespace).List(context.Background(), tests.UnfinishedVMIPodSelector(firstVMI))
			Expect(err).ToNot(HaveOccurred())
			Expect(pods.Items).To(HaveLen(1))
			firstPod := pods.Items[0]

			// Delete the Pod
			By("Deleting the VirtualMachineInstance's pod")
			Eventually(func() error {
				return virtClient.CoreV1().Pods(newVM.Namespace).Delete(context.Background(), firstPod.Name, k8smetav1.DeleteOptions{})
			}, 120*time.Second, 1*time.Second).Should(Succeed())

			// Wait on the VMI controller to create a new VirtualMachineInstance
			By("Waiting for a new VirtualMachineInstance to spawn")
			Eventually(ThisVMIWith(newVM.Namespace, newVM.Name), 120).Should(BeRestarted(firstVMI.UID))

			// sanity check that the test ran correctly by
			// verifying a different Pod backs the VMI as well.
			By("Verifying a new pod backs the VMI")
			curVMI, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			pods, err = virtClient.CoreV1().Pods(newVM.Namespace).List(context.Background(), tests.UnfinishedVMIPodSelector(curVMI))
			Expect(err).ToNot(HaveOccurred())
			Expect(pods.Items).To(HaveLen(1))
			pod := pods.Items[0]
			Expect(pod.Name).ToNot(Equal(firstPod.Name))
		})

		DescribeTable("[test_id:1525]should stop VirtualMachineInstance if running set to false", func(createTemplate vmiBuilder) {
			template, dv := createTemplate()
			defer libstorage.DeleteDataVolume(&dv)
			vm := startVM(createVM(template, false))
			stopVM(vm)
		},
			Entry("with ContainerDisk", newVirtualMachineInstanceWithContainerDisk),
			Entry("[Serial][storage-req]with Filesystem Disk", newVirtualMachineInstanceWithFileDisk),
			Entry("[Serial][storage-req]with Block Disk", newVirtualMachineInstanceWithBlockDisk),
		)

		It("[test_id:1526]should start and stop VirtualMachineInstance multiple times", func() {
			vm := createVM(libvmi.NewCirros(), false)
			// Start and stop VirtualMachineInstance multiple times
			for i := 0; i < 5; i++ {
				By(fmt.Sprintf("Doing run: %d", i))
				startVM(vm)
				stopVM(vm)
			}
		})

		It("[test_id:1527]should not update the VirtualMachineInstance spec if Running", func() {
			newVM := startVM(createVM(libvmi.NewCirros(), false))

			By("Updating the VM template spec")
			updatedVM := newVM.DeepCopy()
			updatedVM.Spec.Template.Spec.Domain.Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4096Ki"),
			}
			updatedVM, err := virtClient.VirtualMachine(updatedVM.Namespace).Update(updatedVM)
			Expect(err).ToNot(HaveOccurred())

			By("Expecting the old VirtualMachineInstance spec still running")
			vmi, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			vmiMemory := vmi.Spec.Domain.Resources.Requests.Memory()
			vmMemory := newVM.Spec.Template.Spec.Domain.Resources.Requests.Memory()
			Expect(vmiMemory.Cmp(*vmMemory)).To(Equal(0))

			By("Restarting the VM")
			newVM = stopVM(newVM)
			newVM = startVM(newVM)

			By("Expecting updated spec running")
			vmi, err = virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			vmiMemory = vmi.Spec.Domain.Resources.Requests.Memory()
			vmMemory = updatedVM.Spec.Template.Spec.Domain.Resources.Requests.Memory()
			Expect(vmiMemory.Cmp(*vmMemory)).To(Equal(0))
		})

		It("[test_id:1528]should survive guest shutdown, multiple times", func() {
			By("Start new VM")
			newVM := startVM(createVM(libvmi.NewCirros(), false))

			for i := 0; i < 3; i++ {
				By("Getting the running VirtualMachineInstance")
				currentVMI, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				By("Obtaining the serial console")
				Expect(console.LoginToCirros(currentVMI)).To(Succeed())

				By("Guest shutdown")
				Expect(console.SafeExpectBatch(currentVMI, []expect.Batcher{
					&expect.BSnd{S: "sudo poweroff\n"},
					&expect.BExp{R: "The system is going down NOW!"},
				}, 240)).To(Succeed())

				By("waiting for the controller to replace the shut-down vmi with a new instance")
				Eventually(func() bool {
					vmi, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
					// Almost there, a new instance should be spawned soon
					if errors.IsNotFound(err) {
						return false
					}
					Expect(err).ToNot(HaveOccurred())
					// If the UID of the vmi changed we see the new vmi
					if vmi.UID != currentVMI.UID {
						return true
					}
					return false
				}, 240*time.Second, 1*time.Second).Should(BeTrue(), "No new VirtualMachineInstance instance showed up")

				By("VMI should run the VirtualMachineInstance again")
			}
		})

		It("should create vm revision when starting vm", func() {
			newVM := startVM(createVM(libvmi.NewCirros(), false))

			vmi, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			expectedVMRevisionName := fmt.Sprintf("revision-start-vm-%s-%d", newVM.UID, newVM.Generation)
			Expect(vmi.Status.VirtualMachineRevisionName).To(Equal(expectedVMRevisionName))

			cr, err := virtClient.AppsV1().ControllerRevisions(newVM.Namespace).Get(context.Background(), vmi.Status.VirtualMachineRevisionName, k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(cr.Revision).To(Equal(int64(2)))
			vmRevision := &v1.VirtualMachine{}
			err = json.Unmarshal(cr.Data.Raw, vmRevision)
			Expect(err).ToNot(HaveOccurred())

			Expect(vmRevision.Spec).To(Equal(newVM.Spec))
		})

		It("should delete old vm revision and create new one when restarting vm", func() {
			By("Starting the VM")
			newVM := startVM(createVM(libvmi.NewCirros(), false))

			vmi, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			expectedVMRevisionName := fmt.Sprintf("revision-start-vm-%s-%d", newVM.UID, newVM.Generation)
			Expect(vmi.Status.VirtualMachineRevisionName).To(Equal(expectedVMRevisionName))
			oldVMRevisionName := expectedVMRevisionName

			By("Stopping the VM")
			newVM = stopVM(newVM)

			By("Updating the VM template spec")
			newVM.Spec.Template.Spec.Domain.Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("4096Ki"),
			}
			newVM, err = virtClient.VirtualMachine(newVM.Namespace).Update(newVM)
			Expect(err).ToNot(HaveOccurred())

			By("Starting the VM after update")
			newVM = startVM(newVM)

			vmi, err = virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			expectedVMRevisionName = fmt.Sprintf("revision-start-vm-%s-%d", newVM.UID, newVM.Generation)
			Expect(vmi.Status.VirtualMachineRevisionName).To(Equal(expectedVMRevisionName))

			cr, err := virtClient.AppsV1().ControllerRevisions(newVM.Namespace).Get(context.Background(), oldVMRevisionName, k8smetav1.GetOptions{})
			Expect(errors.IsNotFound(err)).To(BeTrue())

			cr, err = virtClient.AppsV1().ControllerRevisions(newVM.Namespace).Get(context.Background(), vmi.Status.VirtualMachineRevisionName, k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(cr.Revision).To(Equal(int64(5)))
			vmRevision := &v1.VirtualMachine{}
			err = json.Unmarshal(cr.Data.Raw, vmRevision)
			Expect(err).ToNot(HaveOccurred())

			Expect(vmRevision.Spec).To(Equal(newVM.Spec))
		})

		It("[test_id:4645]should set the Ready condition on VM", func() {
			vm := createVM(libvmi.NewCirros(), false)

			vmReadyConditionStatus := func() k8sv1.ConditionStatus {
				updatedVm, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				cond := controller.NewVirtualMachineConditionManager().
					GetCondition(updatedVm, v1.VirtualMachineReady)
				if cond == nil {
					return ""
				}
				return cond.Status
			}

			Expect(vmReadyConditionStatus()).ToNot(Equal(k8sv1.ConditionTrue))

			startVM(vm)

			Eventually(vmReadyConditionStatus, 300*time.Second, 1*time.Second).
				Should(Equal(k8sv1.ConditionTrue))

			stopVM(vm)

			Eventually(vmReadyConditionStatus, 300*time.Second, 1*time.Second).
				Should(Equal(k8sv1.ConditionFalse))
		})

		DescribeTable("should report an error status when VM scheduling error occurs", func(unschedulableFunc func(vmi *v1.VirtualMachineInstance)) {
			vmi := libvmi.New(
				libvmi.WithContainerImage("no-such-image"),
				libvmi.WithResourceMemory("128Mi"),
			)
			unschedulableFunc(vmi)
			vm := createVM(vmi, true)

			vmPrintableStatus := func() v1.VirtualMachinePrintableStatus {
				updatedVm, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return updatedVm.Status.PrintableStatus
			}

			By("Verifying that the VM status eventually gets set to FailedUnschedulable")
			Eventually(vmPrintableStatus, 300*time.Second, 1*time.Second).
				Should(Equal(v1.VirtualMachineStatusUnschedulable))
		},
			Entry("[test_id:6867]with unsatisfiable resource requirements", func(vmi *v1.VirtualMachineInstance) {
				vmi.Spec.Domain.Resources.Requests = corev1.ResourceList{
					// This may stop working sometime around 2040
					corev1.ResourceMemory: resource.MustParse("1Ei"),
					corev1.ResourceCPU:    resource.MustParse("1M"),
				}
			}),
			Entry("[test_id:6868]with unsatisfiable scheduling constraints", func(vmi *v1.VirtualMachineInstance) {
				vmi.Spec.NodeSelector = map[string]string{
					"node-label": "that-doesnt-exist",
				}
			}),
		)

		It("[test_id:6869]should report an error status when image pull error occurs", func() {
			vmi := libvmi.New(
				libvmi.WithContainerImage("no-such-image"),
				libvmi.WithResourceMemory("128Mi"),
			)
			vm := createVM(vmi, true)

			vmPrintableStatus := func() v1.VirtualMachinePrintableStatus {
				updatedVm, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return updatedVm.Status.PrintableStatus
			}

			By("Verifying that the status toggles between ErrImagePull and ImagePullBackOff")
			const times = 2
			for i := 0; i < times; i++ {
				Eventually(vmPrintableStatus, 300*time.Second, 1*time.Second).
					Should(Equal(v1.VirtualMachineStatusErrImagePull))

				Eventually(vmPrintableStatus, 300*time.Second, 1*time.Second).
					Should(Equal(v1.VirtualMachineStatusImagePullBackOff))
			}
		})

		DescribeTable("should report an error status when a VM with a missing PVC/DV is started", func(vmiFunc func() *v1.VirtualMachineInstance, status v1.VirtualMachinePrintableStatus) {
			vm := createVM(vmiFunc(), true)

			vmPrintableStatus := func() v1.VirtualMachinePrintableStatus {
				updatedVm, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return updatedVm.Status.PrintableStatus
			}

			Eventually(vmPrintableStatus, 300*time.Second, 1*time.Second).Should(Equal(status))
		},
			Entry(
				"[test_id:7596]missing PVC",
				func() *v1.VirtualMachineInstance {
					return libvmi.New(
						libvmi.WithPersistentVolumeClaim("disk0", "missing-pvc"),
						libvmi.WithResourceMemory("128Mi"),
					)
				},
				v1.VirtualMachineStatusPvcNotFound,
			),
			Entry(
				"[test_id:7597]missing DataVolume",
				func() *v1.VirtualMachineInstance {
					return libvmi.New(
						libvmi.WithDataVolume("disk0", "missing-datavolume"),
						libvmi.WithResourceMemory("128Mi"),
					)
				},
				v1.VirtualMachineStatusPvcNotFound,
			),
		)

		It("[test_id:7679]should report an error status when data volume error occurs", func() {
			By("Verifying that required StorageClass is configured")
			storageClassName := libstorage.Config.StorageRWOFileSystem

			_, err := virtClient.StorageV1().StorageClasses().Get(context.Background(), storageClassName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				Skip("Skipping since required StorageClass is not configured")
			}
			Expect(err).ToNot(HaveOccurred())

			By("Creating a VM with a DataVolume cloned from an invalid source")
			// Registry URL scheme validated in CDI
			vm := tests.NewRandomVMWithDataVolumeWithRegistryImport("docker://no.such/image",
				util.NamespaceTestDefault, storageClassName, k8sv1.ReadWriteOnce)
			vm.Spec.Running = pointer.BoolPtr(true)
			_, err = virtClient.VirtualMachine(vm.Namespace).Create(vm)
			Expect(err).ToNot(HaveOccurred())

			vmPrintableStatus := func() v1.VirtualMachinePrintableStatus {
				updatedVm, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return updatedVm.Status.PrintableStatus
			}

			By("Verifying that the VM status eventually gets set to DataVolumeError")
			Eventually(vmPrintableStatus, 300*time.Second, 1*time.Second).
				Should(Equal(v1.VirtualMachineStatusDataVolumeError))
		})

		Context("Using virtctl interface", func() {
			It("[test_id:1529]should start a VirtualMachineInstance once", func() {
				By("getting a VM")
				newVM := createVM(libvmi.NewCirros(), false)

				By("Invoking virtctl start")
				startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", newVM.Namespace, newVM.Name)
				Expect(startCommand()).To(Succeed())

				By("Getting the status of the VM")
				Eventually(ThisVM(newVM), 360).Should(BeReady())

				By("Getting the running VirtualMachineInstance")
				Eventually(ThisVMIWith(newVM.Namespace, newVM.Name), 120).Should(BeRunning())

				By("Ensuring a second invocation should fail")
				err = startCommand()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(fmt.Sprintf(`Error starting VirtualMachine Operation cannot be fulfilled on virtualmachine.kubevirt.io "%s": VM is already running`, newVM.Name)))
			})

			It("[test_id:1530]should stop a VirtualMachineInstance once", func() {
				By("getting a VM")
				newVM := startVM(createVM(libvmi.NewCirros(), false))

				By("Invoking virtctl stop")
				stopCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_STOP, "--namespace", newVM.Namespace, newVM.Name)
				Expect(stopCommand()).To(Succeed())

				By("Ensuring VM is not running")
				Eventually(ThisVM(newVM), 360).ShouldNot(And(BeReady(), BeCreated()))

				By("Ensuring the VirtualMachineInstance is removed")
				Eventually(ThisVMIWith(newVM.Namespace, newVM.Name), 240).ShouldNot(Exist())

				By("Ensuring a second invocation should fail")
				err = stopCommand()
				Expect(err).ToNot(Succeed())
				Expect(err.Error()).To(Equal(fmt.Sprintf(`Error stopping VirtualMachine Operation cannot be fulfilled on virtualmachine.kubevirt.io "%s": VM is not running`, newVM.Name)))
			})

			It("[test_id:6310]should start a VirtualMachineInstance in paused state", func() {
				By("getting a VM")
				newVM := createVM(libvmi.NewCirros(), false)

				By("Invoking virtctl start")
				startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", newVM.Namespace, newVM.Name, "--paused")
				Expect(startCommand()).To(Succeed())

				By("Getting the status of the VM")
				Eventually(ThisVM(newVM), 360).Should(BeCreated())

				By("Getting running VirtualMachineInstance with paused condition")
				Eventually(func() bool {
					vmi, err := virtClient.VirtualMachineInstance(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(*vmi.Spec.StartStrategy).To(Equal(v1.StartStrategyPaused))
					tests.WaitForVMICondition(virtClient, vmi, v1.VirtualMachineInstancePaused, 30)
					return vmi.Status.Phase == v1.Running
				}, 240*time.Second, 1*time.Second).Should(BeTrue())
			})

			It("[test_id:3007]Should force restart a VM with terminationGracePeriodSeconds>0", func() {
				By("getting a VM with high TerminationGracePeriod")
				vm := startVM(createVM(libvmi.NewFedora(
					libvmi.WithTerminationGracePeriod(600),
				), false))

				vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				By("Invoking virtctl --force restart")
				forceRestart := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_RESTART, "--namespace", vm.Namespace, "--force", vm.Name, "--grace-period=0")
				err = forceRestart()
				Expect(err).ToNot(HaveOccurred())

				// Checks if the old VMI Pod still exists after force-restart command
				zeroGracePeriod := int64(0)
				Eventually(func() string {
					pod, err := tests.GetRunningPodByLabel(string(vmi.UID), v1.CreatedByLabel, vm.Namespace, "")
					if err != nil {
						return err.Error()
					}
					if pod.GetDeletionGracePeriodSeconds() == &zeroGracePeriod && pod.GetDeletionTimestamp() != nil {
						return "old VMI Pod still not deleted"
					}
					return ""
				}, 120*time.Second, 1*time.Second).Should(ContainSubstring("failed to find pod"))

				Eventually(ThisVMI(vmi), 240).Should(BeRestarted(vmi.UID))

				By("Comparing the new UID and CreationTimeStamp with the old ones")
				newVMI, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(newVMI.UID).ToNot(Equal(vmi.UID))
				Expect(newVMI.CreationTimestamp).ToNot(Equal(vmi.CreationTimestamp))
			})

			It("Should force stop a VMI", func() {
				By("getting a VM with high TerminationGracePeriod")
				newVM := startVM(createVM(libvmi.New(
					libvmi.WithResourceMemory("128Mi"),
					libvmi.WithTerminationGracePeriod(1600),
				), false))

				By("setting up a watch for vmi")
				lw, err := virtClient.VirtualMachineInstance(newVM.Namespace).Watch(metav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred())

				terminationGracePeriodUpdated := func(stopCn <-chan bool, eventsCn <-chan watch.Event, updated chan<- bool) {
					for {
						select {
						case <-stopCn:
							return
						case e := <-eventsCn:
							vmi, ok := e.Object.(*v1.VirtualMachineInstance)
							Expect(ok).To(BeTrue())
							if vmi.Name != newVM.Name {
								continue
							}

							if *vmi.Spec.TerminationGracePeriodSeconds == 0 {
								updated <- true
							}
						}
					}
				}
				stopCn := make(chan bool, 1)
				updated := make(chan bool, 1)
				go terminationGracePeriodUpdated(stopCn, lw.ResultChan(), updated)

				By("Invoking virtctl --force stop")
				forceStop := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_STOP, newVM.Name, "--namespace", newVM.Namespace, "--force", "--grace-period=0")
				Expect(forceStop()).ToNot(HaveOccurred())

				By("Ensuring the VirtualMachineInstance is removed")
				Eventually(ThisVMIWith(newVM.Namespace, newVM.Name), 240).ShouldNot(Exist())

				Expect(updated).To(Receive(), "vmi should be updated")
				stopCn <- true
			})

			Context("Using RunStrategyAlways", func() {
				It("[test_id:3163]should stop a running VM", func() {
					By("creating a VM with RunStrategyAlways")
					vm := createCirrosWithRunStrategy(v1.RunStrategyAlways)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Invoking virtctl stop")
					stopCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_STOP, "--namespace", vm.Namespace, vm.Name)
					Expect(stopCommand()).To(Succeed())

					By("Ensuring the VirtualMachineInstance is removed")
					Eventually(ThisVMIWith(vm.Namespace, vm.Name), 240).ShouldNot(Exist())

					newVM, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(newVM.Spec.RunStrategy).ToNot(BeNil())
					Expect(*newVM.Spec.RunStrategy).To(Equal(v1.RunStrategyHalted))
					Expect(newVM.Status.StateChangeRequests).To(BeEmpty())
				})

				It("[test_id:3164]should restart a running VM", func() {
					By("creating a VM with RunStrategyAlways")
					vm := createCirrosWithRunStrategy(v1.RunStrategyAlways)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Getting VMI's UUID")
					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())

					By("Invoking virtctl restart")
					restartCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_RESTART, "--namespace", vm.Namespace, vm.Name)
					Expect(restartCommand()).To(Succeed())

					By("Ensuring the VirtualMachineInstance is restarted")
					Eventually(ThisVMI(vmi), 240).Should(BeRestarted(vmi.UID))

					newVM, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(newVM.Spec.RunStrategy).ToNot(BeNil())
					Expect(*newVM.Spec.RunStrategy).To(Equal(v1.RunStrategyAlways))

					// StateChangeRequest might still exist until the new VMI is created
					// But it must eventually be cleared
					Eventually(ThisVM(vm), 240).ShouldNot(HaveStateChangeRequests())
				})

				It("[test_id:3165]should restart a succeeded VMI", func() {
					By("creating a VM with RunStategyRunning")
					vm := createCirrosWithRunStrategy(v1.RunStrategyAlways)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())

					Expect(console.LoginToCirros(vmi)).To(Succeed())

					By("Issuing a poweroff command from inside VM")
					Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
						&expect.BSnd{S: "sudo poweroff\n"},
						&expect.BExp{R: console.PromptExpression},
					}, 10)).To(Succeed())

					By("Ensuring the VirtualMachineInstance is restarted")
					Eventually(ThisVMI(vmi), 240).Should(BeRestarted(vmi.UID))
				})

				It("[test_id:4119]should migrate a running VM", func() {
					nodes := libnode.GetAllSchedulableNodes(virtClient)
					if len(nodes.Items) < 2 {
						Skip("Migration tests require at least 2 nodes")
					}
					By("creating a VM with RunStrategyAlways")
					vm := createCirrosWithRunStrategy(v1.RunStrategyAlways)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Invoking virtctl migrate")
					migrateCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_MIGRATE, "--namespace", vm.Namespace, vm.Name)
					Expect(migrateCommand()).To(Succeed())

					By("Ensuring the VirtualMachineInstance is migrated")
					Eventually(func() bool {
						nextVMI, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
						Expect(err).ToNot(HaveOccurred())
						return nextVMI.Status.MigrationState != nil && nextVMI.Status.MigrationState.Completed
					}, 240*time.Second, 1*time.Second).Should(BeTrue())
				})

				It("[test_id:7743]should not migrate a running vm if dry-run option is passed", func() {
					nodes := libnode.GetAllSchedulableNodes(virtClient)
					if len(nodes.Items) < 2 {
						Skip("Migration tests require at least 2 nodes")
					}
					By("creating a VM with RunStrategyAlways")
					vm := createCirrosWithRunStrategy(v1.RunStrategyAlways)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Invoking virtctl migrate with dry-run option")
					migrateCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_MIGRATE, "--dry-run", "--namespace", vm.Namespace, vm.Name)
					Expect(migrateCommand()).To(Succeed())

					By("Check that no migration was actually created")
					Consistently(func() bool {
						_, err = virtClient.VirtualMachineInstanceMigration(vm.Namespace).Get(vm.Name, &metav1.GetOptions{})
						return errors.IsNotFound(err)
					}, 60*time.Second, 5*time.Second).Should(BeTrue(), "migration should not be created in a dry run mode")
				})
			})

			Context("Using RunStrategyRerunOnFailure", func() {
				It("[test_id:2186] should stop a running VM", func() {
					By("creating a VM with RunStrategyRerunOnFailure")
					vm := createCirrosWithRunStrategy(v1.RunStrategyRerunOnFailure)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Invoking virtctl stop")
					stopCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_STOP, "--namespace", vm.Namespace, vm.Name)
					err = stopCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Ensuring the VirtualMachineInstance is removed")
					Eventually(ThisVMIWith(vm.Namespace, vm.Name), 240).ShouldNot(Exist())

					vm, err = virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(vm.Spec.RunStrategy).ToNot(BeNil())
					Expect(*vm.Spec.RunStrategy).To(Equal(v1.RunStrategyHalted))
					By("Ensuring stateChangeRequests list is cleared")
					Expect(vm.Status.StateChangeRequests).To(BeEmpty())
				})

				It("[test_id:2187] should restart a running VM", func() {
					By("creating a VM with RunStrategyRerunOnFailure")
					vm := createCirrosWithRunStrategy(v1.RunStrategyRerunOnFailure)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Getting VMI's UUID")
					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())

					By("Invoking virtctl restart")
					restartCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_RESTART, "--namespace", vm.Namespace, vm.Name)
					err = restartCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Ensuring the VirtualMachineInstance is restarted")
					Eventually(ThisVMI(vmi), 240).Should(BeRestarted(vmi.UID))

					vm, err = virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(vm.Spec.RunStrategy).ToNot(BeNil())
					Expect(*vm.Spec.RunStrategy).To(Equal(v1.RunStrategyRerunOnFailure))

					// StateChangeRequest might still exist until the new VMI is created
					// But it must eventually be cleared
					Eventually(ThisVM(vm), 240).ShouldNot(HaveStateChangeRequests())
				})

				It("[test_id:2188] should not remove a succeeded VMI", func() {
					By("creating a VM with RunStrategyRerunOnFailure")
					vm := createCirrosWithRunStrategy(v1.RunStrategyRerunOnFailure)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(console.LoginToCirros(vmi)).To(Succeed())

					By("Issuing a poweroff command from inside VM")
					Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
						&expect.BSnd{S: "sudo poweroff\n"},
						&expect.BExp{R: console.PromptExpression},
					}, 10)).To(Succeed())

					By("Ensuring the VirtualMachineInstance enters Succeeded phase")
					Eventually(ThisVMIWith(vm.Namespace, vm.Name), 240).Should(HaveSucceeded())

					// At this point, explicitly test that a start command will delete an existing
					// VMI in the Succeeded phase.
					By("Invoking virtctl start")
					restartCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					err = restartCommand()
					Expect(err).ToNot(HaveOccurred())

					Eventually(ThisVM(vm), 240).ShouldNot(HaveStateChangeRequests())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())
				})
			})

			Context("Using RunStrategyHalted", func() {
				It("[test_id:2037] should start a stopped VM", func() {
					By("creating a VM with RunStrategyHalted")
					vm := createCirrosWithRunStrategy(v1.RunStrategyHalted)

					startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					err = startCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					vm, err = virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(vm.Spec.RunStrategy).ToNot(BeNil())
					Expect(*vm.Spec.RunStrategy).To(Equal(v1.RunStrategyAlways))
					By("Ensuring stateChangeRequests list is cleared")
					Expect(vm.Status.StateChangeRequests).To(BeEmpty())
				})
			})

			Context("Using RunStrategyOnce", func() {
				It("[Serial] Should leave a failed VMI", func() {
					By("creating a VM with RunStrategyOnce")
					vm := createCirrosWithRunStrategy(v1.RunStrategyOnce)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())

					By("killing qemu process")
					err = pkillAllVMIs(virtClient, vmi.Status.NodeName)
					Expect(err).ToNot(HaveOccurred(), "Should kill VMI successfully")

					By("Ensuring the VirtualMachineInstance enters Failed phase")
					Eventually(ThisVMIWith(vm.Namespace, vm.Name), 240).Should(BeInPhase(v1.Failed))

					By("Ensuring the VirtualMachine remains stopped")
					Consistently(ThisVMIWith(vm.Namespace, vm.Name), 60, 5).Should(BeInPhase(v1.Failed))

					By("Ensuring the VirtualMachine remains Ready=false")
					Expect(ThisVM(vm)).ToNot(BeReady())
				})

				It("Should leave a succeeded VMI", func() {
					By("creating a VM with RunStrategyOnce")
					vm := createCirrosWithRunStrategy(v1.RunStrategyOnce)

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(console.LoginToCirros(vmi)).To(Succeed())

					By("Issuing a poweroff command from inside VM")
					Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
						&expect.BSnd{S: "sudo poweroff\n"},
						&expect.BExp{R: console.PromptExpression},
					}, 10)).To(Succeed())

					By("Ensuring the VirtualMachineInstance enters Succeeded phase")
					Eventually(ThisVMI(vmi), 240).Should(HaveSucceeded())

					By("Ensuring the VirtualMachine remains stopped")
					Consistently(ThisVMI(vmi), 60, 5).Should(HaveSucceeded())

					By("Ensuring the VirtualMachine remains Ready=false")
					Expect(ThisVM(vm)).ToNot(BeReady())
				})
			})

			Context("Using RunStrategyManual", func() {
				It("[test_id:2036] should start", func() {
					By("creating a VM with RunStrategyManual")
					vm := createCirrosWithRunStrategy(v1.RunStrategyManual)

					startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					err = startCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					vm, err = virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(vm.Spec.RunStrategy).ToNot(BeNil())
					Expect(*vm.Spec.RunStrategy).To(Equal(v1.RunStrategyManual))
					By("Ensuring stateChangeRequests list is cleared")
					Expect(vm.Status.StateChangeRequests).To(BeEmpty())
				})

				It("[test_id:2189] should stop", func() {
					By("creating a VM with RunStrategyManual")
					vm := createCirrosWithRunStrategy(v1.RunStrategyManual)

					startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					err = startCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					stopCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_STOP, "--namespace", vm.Namespace, vm.Name)
					err = stopCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Ensuring the VirtualMachineInstance is removed")
					Eventually(ThisVMIWith(vm.Namespace, vm.Name), 240).ShouldNot(Exist())

					Eventually(ThisVM(vm), 240).ShouldNot(HaveStateChangeRequests())
				})

				It("[test_id:6311]should start in paused state", func() {
					By("creating a VM with RunStrategyManual")
					vm := createCirrosWithRunStrategy(v1.RunStrategyManual)

					By("Invoking virtctl start")
					startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name, "--paused")
					Expect(startCommand()).To(Succeed())

					By("Getting the status of the VM")
					Eventually(ThisVM(vm), 360).Should(BeCreated())

					By("Getting running VirtualMachineInstance with paused condition")
					Eventually(func() bool {
						vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
						Expect(err).ToNot(HaveOccurred())
						Expect(*vmi.Spec.StartStrategy).To(Equal(v1.StartStrategyPaused))
						tests.WaitForVMICondition(virtClient, vmi, v1.VirtualMachineInstancePaused, 30)
						return vmi.Status.Phase == v1.Running
					}, 240*time.Second, 1*time.Second).Should(BeTrue())
				})

				It("[test_id:2035] should restart", func() {
					By("creating a VM with RunStrategyManual")
					vm := createCirrosWithRunStrategy(v1.RunStrategyManual)

					startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					stopCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_STOP, "--namespace", vm.Namespace, vm.Name)
					restartCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_RESTART, "--namespace", vm.Namespace, vm.Name)

					By("Invoking virtctl restart should fail")
					err = restartCommand()
					Expect(err).To(HaveOccurred())

					By("Invoking virtctl start")
					err = startCommand()
					Expect(err).NotTo(HaveOccurred())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Invoking virtctl stop")
					err = stopCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Ensuring the VirtualMachineInstance is stopped")
					Eventually(ThisVM(vm), 240).ShouldNot(BeCreated())

					Eventually(ThisVM(vm), 240).ShouldNot(HaveStateChangeRequests())

					By("Invoking virtctl start")
					err = startCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					By("Getting VMI's UUID")
					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())

					By("Invoking virtctl restart")
					err = restartCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Ensuring the VirtualMachineInstance is restarted")
					Eventually(ThisVMI(vmi), 240).Should(BeRestarted(vmi.UID))

					vm, err = virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(vm.Spec.RunStrategy).ToNot(BeNil())
					Expect(*vm.Spec.RunStrategy).To(Equal(v1.RunStrategyManual))

					Eventually(ThisVM(vm), 240).ShouldNot(HaveStateChangeRequests())
				})

				It("[test_id:2190] should not remove a succeeded VMI", func() {
					By("creating a VM with RunStrategyManual")
					vm := createCirrosWithRunStrategy(v1.RunStrategyManual)

					startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					err = startCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())

					vmi, err := virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					Expect(console.LoginToCirros(vmi)).To(Succeed())

					By("Issuing a poweroff command from inside VM")
					Expect(console.SafeExpectBatch(vmi, []expect.Batcher{
						&expect.BSnd{S: "sudo poweroff\n"},
						&expect.BExp{R: console.PromptExpression},
					}, 10)).To(Succeed())

					By("Ensuring the VirtualMachineInstance enters Succeeded phase")
					Eventually(ThisVMI(vmi), 240).Should(HaveSucceeded())

					// At this point, explicitly test that a start command will delete an existing
					// VMI in the Succeeded phase.
					By("Invoking virtctl start")
					restartCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					err = restartCommand()
					Expect(err).ToNot(HaveOccurred())

					Eventually(ThisVM(vm), 240).ShouldNot(HaveStateChangeRequests())

					By("Waiting for VM to be ready")
					Eventually(ThisVM(vm), 360).Should(BeReady())
				})
				DescribeTable("with a failing VMI and the kubevirt.io/keep-launcher-alive-after-failure annotation", func(keepLauncher string) {
					// The estimated execution time of one test is 400 seconds.
					By("Creating a Kernel Boot VMI with a mismatched disk")
					vmi := utils.GetVMIKernelBoot()
					vmi.Spec.Domain.Firmware.KernelBoot.Container.Image = cd.ContainerDiskFor(cd.ContainerDiskCirros)

					By("Creating a VM with RunStrategyManual")
					vm := tests.NewRandomVirtualMachine(vmi, false)
					vm.Spec.Running = nil
					vm.Spec.RunStrategy = &runStrategyManual

					By("Annotate the VM with regard for leaving launcher pod after qemu exit")
					vm.Spec.Template.ObjectMeta.Annotations = map[string]string{
						v1.KeepLauncherAfterFailureAnnotation: keepLauncher,
					}
					vm, err := virtClient.VirtualMachine(util.NamespaceTestDefault).Create(vm)
					Expect(err).ToNot(HaveOccurred())

					By("Starting the VMI with virtctl")
					startCommand := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", vm.Namespace, vm.Name)
					err = startCommand()
					Expect(err).ToNot(HaveOccurred())

					By("Waiting for VM to be in Starting status")
					Eventually(func() bool {
						vm, err = virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
						Expect(err).ToNot(HaveOccurred())
						return vm.Status.PrintableStatus == v1.VirtualMachineStatusStarting
					}, 160*time.Second, 1*time.Second).Should(BeTrue())

					Eventually(ThisVMIWith(vm.Namespace, vm.Name), 480).Should(BeInPhase(v1.Failed))

					// If the annotation v1.KeepLauncherAfterFailureAnnotation is set to true, the containerStatus of the
					// compute container of the virt-launcher pod is kept in the running state.
					// If the annotation v1.KeepLauncherAfterFailureAnnotation is set to false or not set, the virt-launcher pod will become failed.
					By("Verify that the virt-launcher pod or its container is in the expected state")
					vmi, err = virtClient.VirtualMachineInstance(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
					Expect(err).ToNot(HaveOccurred())
					launcherPod := libvmi.GetPodByVirtualMachineInstance(vmi, vm.Namespace)

					if toKeep, _ := strconv.ParseBool(keepLauncher); toKeep {
						Consistently(func() bool {
							for _, status := range launcherPod.Status.ContainerStatuses {
								if status.Name == "compute" && status.State.Running != nil {
									return true
								}
							}
							return false
						}, 10*time.Second, 1*time.Second).Should(BeTrue())
					} else {
						Eventually(Expect(launcherPod.Status.Phase).To(Equal(k8sv1.PodFailed)), 160*time.Second, 1*time.Second).Should(BeTrue())
					}
				},
					Entry("[test_id:7164]VMI launcher pod should fail", "false"),
					Entry("[test_id:6993]VMI launcher pod compute container should keep running", "true"),
				)
			})
		})
	})

	Context("[rfe_id:273]with oc/kubectl", func() {
		var k8sClient string
		var workDir string
		var vmRunningRe *regexp.Regexp

		BeforeEach(func() {
			k8sClient = clientcmd.GetK8sCmdClient()
			clientcmd.SkipIfNoCmd(k8sClient)
			workDir = GinkgoT().TempDir()

			// By default "." does not match newline: "Phase" and "Running" only match if on same line.
			vmRunningRe = regexp.MustCompile("Phase.*Running")
		})

		It("[test_id:243][posneg:negative]should create VM only once", func() {
			vm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), true)
			vm.Namespace = util.NamespaceTestDefault

			vmJson, err := tests.GenerateVMJson(vm, workDir)
			Expect(err).ToNot(HaveOccurred(), "Cannot generate VMs manifest")

			By("Creating VM with DataVolumeTemplate entry with k8s client binary")
			_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying VM is created")
			newVM, err := virtClient.VirtualMachine(vm.Namespace).Get(vm.Name, &k8smetav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred(), "New VM was not created")
			Expect(newVM.Name).To(Equal(vm.Name), "New VM was not created")

			By("Creating the VM again")
			_, stdErr, err := clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
			Expect(err).To(HaveOccurred())

			Expect(strings.HasPrefix(stdErr, "Error from server (AlreadyExists): error when creating")).To(BeTrue(), "command should error when creating VM second time")
		})

		DescribeTable("[release-blocker][test_id:299]should create VM via command line using all supported API versions", func(version string) {
			vm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), true)
			vm.Namespace = util.NamespaceTestDefault
			vm.APIVersion = version

			vmJson, err := tests.GenerateVMJson(vm, workDir)
			Expect(err).ToNot(HaveOccurred(), "Cannot generate VMs manifest")

			By("Creating VM using k8s client binary")
			_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for VMI to start")
			Eventually(ThisVMIWith(vm.Namespace, vm.Name), 120).Should(BeRunning())

			By("Listing running pods")
			stdout, _, err := clientcmd.RunCommand(k8sClient, "get", "pods")
			Expect(err).ToNot(HaveOccurred())

			By("Ensuring pod is running")
			expectedPodName := getExpectedPodName(vm)
			podRunningRe, err := regexp.Compile(fmt.Sprintf("%s.*Running", expectedPodName))
			Expect(err).ToNot(HaveOccurred())

			Expect(podRunningRe.FindString(stdout)).ToNot(Equal(""), "Pod is not Running")

			By("Checking that VM is running")
			stdout, _, err = clientcmd.RunCommand(k8sClient, "describe", "vmis", vm.GetName())
			Expect(err).ToNot(HaveOccurred())

			Expect(vmRunningRe.FindString(stdout)).ToNot(Equal(""), "VMI is not Running")
		},
			Entry("with v1 api", "kubevirt.io/v1"),
			Entry("with v1alpha3 api", "kubevirt.io/v1alpha3"),
		)

		It("[test_id:264]should create and delete via command line", func() {
			thisVm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), false)
			thisVm.Namespace = util.NamespaceTestDefault

			vmJson, err := tests.GenerateVMJson(thisVm, workDir)
			Expect(err).ToNot(HaveOccurred(), "Cannot generate VM's manifest")

			By("Creating VM using k8s client binary")
			_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
			Expect(err).ToNot(HaveOccurred())

			By("Invoking virtctl start")
			virtctl := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", thisVm.Namespace, thisVm.Name)
			err = virtctl()
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for VMI to start")
			Eventually(ThisVMIWith(thisVm.Namespace, thisVm.Name), 120).Should(BeRunning())

			By("Checking that VM is running")
			stdout, _, err := clientcmd.RunCommand(k8sClient, "describe", "vmis", thisVm.GetName())
			Expect(err).ToNot(HaveOccurred())

			Expect(vmRunningRe.FindString(stdout)).ToNot(Equal(""), "VMI is not Running")

			By("Deleting VM using k8s client binary")
			_, _, err = clientcmd.RunCommand(k8sClient, "delete", "vm", thisVm.GetName())
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the VM gets deleted")
			waitForResourceDeletion(k8sClient, "vms", thisVm.GetName())

			By("Verifying pod gets deleted")
			expectedPodName := getExpectedPodName(thisVm)
			waitForResourceDeletion(k8sClient, "pods", expectedPodName)
		})

		Context("should not change anything if dry-run option is passed", func() {
			It("[test_id:7530]in start command", func() {
				thisVm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), false)
				thisVm.Namespace = util.NamespaceTestDefault

				vmJson, err := tests.GenerateVMJson(thisVm, workDir)
				Expect(err).ToNot(HaveOccurred(), "Cannot generate VM's manifest")

				By("Creating VM using k8s client binary")
				_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
				Expect(err).ToNot(HaveOccurred())

				By("Invoking virtctl start with dry-run option")
				virtctl := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_START, "--namespace", thisVm.Namespace, "--dry-run", thisVm.Name)
				err = virtctl()
				Expect(err).ToNot(HaveOccurred())

				_, err = virtClient.VirtualMachineInstance(thisVm.Namespace).Get(thisVm.Name, &k8smetav1.GetOptions{})
				Expect(err).To(HaveOccurred())
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})

			DescribeTable("in stop command", func(flags ...string) {
				thisVm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), true)
				thisVm.Namespace = util.NamespaceTestDefault

				vmJson, err := tests.GenerateVMJson(thisVm, workDir)
				Expect(err).ToNot(HaveOccurred(), "Cannot generate VM's manifest")

				By("Creating VM using k8s client binary")
				_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for VMI to start")
				Eventually(ThisVMIWith(thisVm.Namespace, thisVm.Name), 120).Should(BeRunning())

				By("Getting current vmi instance")
				originalVMI, err := virtClient.VirtualMachineInstance(thisVm.Namespace).Get(thisVm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				By("Getting current vm instance")
				originalVM, err := virtClient.VirtualMachine(thisVm.Namespace).Get(thisVm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				var args = []string{virtctl.COMMAND_STOP, "--namespace", thisVm.Namespace, thisVm.Name, "--dry-run"}
				if flags != nil {
					args = append(args, flags...)
				}
				By("Invoking virtctl stop with dry-run option")
				virtctl := clientcmd.NewRepeatableVirtctlCommand(args...)
				err = virtctl()
				Expect(err).ToNot(HaveOccurred())

				By("Checking that VM is still running")
				stdout, _, err := clientcmd.RunCommand(k8sClient, "describe", "vmis", thisVm.GetName())
				Expect(err).ToNot(HaveOccurred())
				Expect(vmRunningRe.FindString(stdout)).ToNot(Equal(""), "VMI is not Running")

				By("Checking VM Running spec does not change")
				actualVm, err := virtClient.VirtualMachine(thisVm.Namespace).Get(thisVm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(actualVm.Spec.Running).To(BeEquivalentTo(originalVM.Spec.Running))
				actualRunStrategy, err := actualVm.RunStrategy()
				Expect(err).ToNot(HaveOccurred())
				originalRunStrategy, err := originalVM.RunStrategy()
				Expect(err).ToNot(HaveOccurred())
				Expect(actualRunStrategy).To(BeEquivalentTo(originalRunStrategy))

				By("Checking VMI TerminationGracePeriodSeconds does not change")
				actualVMI, err := virtClient.VirtualMachineInstance(thisVm.Namespace).Get(thisVm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(actualVMI.Spec.TerminationGracePeriodSeconds).To(BeEquivalentTo(originalVMI.Spec.TerminationGracePeriodSeconds))
				Expect(actualVMI.Status.Phase).To(BeEquivalentTo(originalVMI.Status.Phase))
			},

				Entry("[test_id:7529]with no other flags"),
				Entry("[test_id:7604]with grace period", "--grace-period=10", "--force"),
			)

			It("[test_id:7528]in restart command", func() {
				thisVm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), true)
				thisVm.Namespace = util.NamespaceTestDefault

				vmJson, err := tests.GenerateVMJson(thisVm, workDir)
				Expect(err).ToNot(HaveOccurred(), "Cannot generate VM's manifest")

				By("Creating VM using k8s client binary")
				_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for VMI to start")
				Eventually(ThisVMIWith(thisVm.Namespace, thisVm.Name), 120).Should(BeRunning())

				By("Getting current vmi instance")
				currentVmi, err := virtClient.VirtualMachineInstance(thisVm.Namespace).Get(thisVm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				creationTime := currentVmi.ObjectMeta.CreationTimestamp
				VMIUuid := currentVmi.ObjectMeta.UID

				By("Invoking virtctl restart with dry-run option")
				virtctl := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_RESTART, "--namespace", thisVm.Namespace, "--dry-run", thisVm.Name)
				err = virtctl()
				Expect(err).ToNot(HaveOccurred())

				By("Comparing the CreationTimeStamp and UUID and check no Deletion Timestamp was set")
				newVMI, err := virtClient.VirtualMachineInstance(thisVm.Namespace).Get(thisVm.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(creationTime).To(Equal(newVMI.ObjectMeta.CreationTimestamp))
				Expect(VMIUuid).To(Equal(newVMI.ObjectMeta.UID))
				Expect(newVMI.ObjectMeta.DeletionTimestamp).To(BeNil())

				By("Checking that VM is running")
				stdout, _, err := clientcmd.RunCommand(k8sClient, "describe", "vmis", thisVm.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(vmRunningRe.FindString(stdout)).ToNot(Equal(""), "VMI is not Running")
			})
		})

		It("[test_id:232]should create same manifest twice via command line", func() {
			thisVm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), true)
			thisVm.Namespace = util.NamespaceTestDefault

			vmJson, err := tests.GenerateVMJson(thisVm, workDir)
			Expect(err).ToNot(HaveOccurred(), "Cannot generate VM's manifest")

			By("Creating VM using k8s client binary")
			_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for VMI to start")
			Eventually(ThisVMIWith(thisVm.Namespace, thisVm.Name), 120).Should(BeRunning())

			By("Deleting VM using k8s client binary")
			_, _, err = clientcmd.RunCommand(k8sClient, "delete", "vm", thisVm.Name)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the VM gets deleted")
			waitForResourceDeletion(k8sClient, "vms", thisVm.GetName())

			By("Creating same VM using k8s client binary and same manifest")
			_, _, err = clientcmd.RunCommand(k8sClient, "create", "-f", vmJson)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for VMI to start")
			Eventually(ThisVMIWith(thisVm.Namespace, thisVm.Name), 120).Should(BeRunning())
		})

		It("[test_id:233][posneg:negative]should fail when deleting nonexistent VM", func() {
			vm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), false)
			_, stdErr, err := clientcmd.RunCommand(k8sClient, "delete", "vm", vm.Name)
			Expect(err).To(HaveOccurred())
			Expect(strings.HasPrefix(stdErr, "Error from server (NotFound): virtualmachines.kubevirt.io")).To(BeTrue(), "should fail when deleting non existent VM")
		})

		Context("as ordinary OCP user trough test service account", func() {
			var testUser string

			BeforeEach(func() {
				testUser = "testuser-" + uuid.NewRandom().String()
			})

			Context("should succeed with right rights", func() {
				BeforeEach(func() {
					// kubectl doesn't have "adm" subcommand -- only oc does
					clientcmd.SkipIfNoCmd("oc")
					By("Ensuring the cluster has new test serviceaccount")
					stdOut, stdErr, err := clientcmd.RunCommand(k8sClient, "create", "user", testUser)
					Expect(err).ToNot(HaveOccurred(), "ERR: %s", stdOut+stdErr)

					By("Ensuring user has the admin rights for the test namespace project")
					// This simulates the ordinary user as an admin in this project
					stdOut, stdErr, err = clientcmd.RunCommand(k8sClient, "adm", "policy", "add-role-to-user", "admin", testUser, "--namespace", util.NamespaceTestDefault)
					Expect(err).ToNot(HaveOccurred(), "ERR: %s", stdOut+stdErr)
				})

				AfterEach(func() {
					stdOut, stdErr, err := clientcmd.RunCommand(k8sClient, "adm", "policy", "remove-role-from-user", "admin", testUser, "--namespace", util.NamespaceTestDefault)
					Expect(err).ToNot(HaveOccurred(), "ERR: %s", stdOut+stdErr)

					stdOut, stdErr, err = clientcmd.RunCommand(k8sClient, "delete", "user", testUser)
					Expect(err).ToNot(HaveOccurred(), "ERR: %s", stdOut+stdErr)
				})

				It("[test_id:2839]should create VM via command line", func() {
					vm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), true)

					_, err := tests.GenerateVMJson(vm, workDir)
					Expect(err).ToNot(HaveOccurred(), "Cannot generate VMs manifest")

					By("Checking VM creation permission using k8s client binary")
					stdOut, _, err := clientcmd.RunCommand(k8sClient, "auth", "can-i", "create", "vms", "--as", testUser)
					Expect(err).ToNot(HaveOccurred())
					Expect(strings.TrimSpace(stdOut)).To(Equal("yes"))
				})
			})

			Context("should fail without right rights", func() {
				BeforeEach(func() {
					By("Ensuring the cluster has new test serviceaccount")
					stdOut, stdErr, err := clientcmd.RunCommandWithNS(util.NamespaceTestDefault, k8sClient, "create", "serviceaccount", testUser)
					Expect(err).ToNot(HaveOccurred(), "ERR: %s", stdOut+stdErr)
				})

				AfterEach(func() {
					stdOut, stdErr, err := clientcmd.RunCommandWithNS(util.NamespaceTestDefault, k8sClient, "delete", "serviceaccount", testUser)
					Expect(err).ToNot(HaveOccurred(), "ERR: %s", stdOut+stdErr)
				})

				It("[test_id:2914]should create VM via command line", func() {
					vm := tests.NewRandomVirtualMachine(libvmi.NewAlpine(), true)

					_, err := tests.GenerateVMJson(vm, workDir)
					Expect(err).ToNot(HaveOccurred(), "Cannot generate VMs manifest")

					By("Checking VM creation permission using k8s client binary")
					stdOut, _, err := clientcmd.RunCommand(k8sClient, "auth", "can-i", "create", "vms", "--as", testUser)
					// non-zero exit code
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("exit status 1"))
					Expect(strings.TrimSpace(stdOut)).To(Equal("no"))
				})
			})
		})

	})

	Context("crash loop backoff", func() {
		It("should backoff attempting to create a new VMI when 'runStrategy: Always' during crash loop.", func() {
			By("Creating VirtualMachine")
			vm := tests.NewRandomVirtualMachine(libvmi.NewCirros(), true)
			vm.Spec.Template.ObjectMeta.Annotations = map[string]string{
				v1.FuncTestLauncherFailFastAnnotation: "",
			}
			newVM, err := virtClient.VirtualMachine(util.NamespaceTestDefault).Create(vm)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for crash loop state")
			Eventually(func() error {
				vm, err := virtClient.VirtualMachine(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				if vm.Status.PrintableStatus != v1.VirtualMachineStatusCrashLoopBackOff {
					return fmt.Errorf("Still waiting on crash loop printable status")
				}

				if vm.Status.StartFailure != nil && vm.Status.StartFailure.ConsecutiveFailCount > 0 {
					return nil
				}

				return fmt.Errorf("Still waiting on crash loop to be detected")
			}, 1*time.Minute, 5*time.Second).Should(BeNil())

			By("Testing that the failure count is within the expected range over a period of time")
			maxExpectedFailCount := 3
			Consistently(func() error {
				// get the VM and verify the failure count is less than 4 over a minute,
				// indicating that backoff is occuring
				vm, err := virtClient.VirtualMachine(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				if vm.Status.StartFailure == nil {
					return fmt.Errorf("start failure count not detected")
				} else if vm.Status.StartFailure.ConsecutiveFailCount > maxExpectedFailCount {
					return fmt.Errorf("consecutive fail count is higher than %d", maxExpectedFailCount)
				}

				return nil
			}, 1*time.Minute, 5*time.Second).Should(BeNil())

			By("Updating the VMI template to correct the crash loop")
			Eventually(func() error {
				updatedVM, err := virtClient.VirtualMachine(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				delete(updatedVM.Spec.Template.ObjectMeta.Annotations, v1.FuncTestLauncherFailFastAnnotation)
				_, err = virtClient.VirtualMachine(updatedVM.Namespace).Update(updatedVM)
				return err
			}, 30*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

			By("Waiting on crash loop status to be removed.")
			Eventually(func() error {
				vm, err := virtClient.VirtualMachine(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				if vm.Status.StartFailure == nil {
					return nil
				}

				return fmt.Errorf("Still waiting on crash loop status to be re-initialized")
			}, 5*time.Minute, 5*time.Second).Should(BeNil())
		})

		It("should be able to stop a VM during crashloop backoff when when 'runStrategy: Always' is set", func() {
			By("Creating VirtualMachine")
			curVM := tests.NewRandomVirtualMachine(libvmi.NewCirros(), true)
			curVM.Spec.Template.ObjectMeta.Annotations = map[string]string{
				v1.FuncTestLauncherFailFastAnnotation: "",
			}
			newVM, err := virtClient.VirtualMachine(util.NamespaceTestDefault).Create(curVM)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for crash loop state")
			Eventually(func() error {
				newVM, err := virtClient.VirtualMachine(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())

				if newVM.Status.PrintableStatus != v1.VirtualMachineStatusCrashLoopBackOff {
					return fmt.Errorf("Still waiting on crash loop printable status")
				}

				if newVM.Status.StartFailure != nil && newVM.Status.StartFailure.ConsecutiveFailCount > 0 {
					return nil
				}

				return fmt.Errorf("Still waiting on crash loop to be detected")
			}, 1*time.Minute, 5*time.Second).Should(BeNil())

			By("Invoking virtctl stop while in a crash loop")
			stopCmd := clientcmd.NewRepeatableVirtctlCommand(virtctl.COMMAND_STOP, newVM.Name, "--namespace", newVM.Namespace)
			Expect(stopCmd()).ToNot(HaveOccurred())

			By("Waiting on crash loop status to be removed.")
			Eventually(func() bool {
				vm, err := virtClient.VirtualMachine(newVM.Namespace).Get(newVM.Name, &k8smetav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return vm.Status.StartFailure == nil
			}, 2*time.Minute, 5*time.Second).Should(BeTrue())
		})
	})
})

func getExpectedPodName(vm *v1.VirtualMachine) string {
	maxNameLength := 63
	podNamePrefix := "virt-launcher-"
	podGeneratedSuffixLen := 5
	charCountFromName := maxNameLength - len(podNamePrefix) - podGeneratedSuffixLen
	expectedPodName := fmt.Sprintf(fmt.Sprintf("virt-launcher-%%.%ds", charCountFromName), vm.GetName())
	return expectedPodName
}

func waitForResourceDeletion(k8sClient string, resourceType string, resourceName string) {
	Eventually(func() bool {
		stdout, _, err := clientcmd.RunCommand(k8sClient, "get", resourceType)
		Expect(err).ToNot(HaveOccurred())
		return strings.Contains(stdout, resourceName)
	}, 120*time.Second, 1*time.Second).Should(BeFalse(), "VM was not deleted")
}
