package cdexport

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/spf13/cobra"
	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	virtv1 "kubevirt.io/api/core/v1"
	exportv1 "kubevirt.io/api/export/v1alpha1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/virtctl/templates"
	"kubevirt.io/kubevirt/pkg/virtctl/vmexport"
)

const (
	CDEXPORT = "cdexport"

	VmFlag   = "vm"
	CdFlag   = "cd"
	NameFlag = "name"

	ManifestAnnotation = "kubevirt.io/vm-manifest"

	suffixImg   = ".img"
	suffixImgGz = ".img.gz"
	suffixYaml  = ".yaml"
	prefixPath  = "disk/"
	imgArch     = "amd64"
	qemuName    = "qemu"
	qemuId      = 107
)

type exportCd struct {
	clientConfig clientcmd.ClientConfig

	vm   string
	cd   string
	name string
}

func NewCommand(clientConfig clientcmd.ClientConfig) *cobra.Command {
	e := exportCd{
		clientConfig: clientConfig,
	}
	cmd := &cobra.Command{
		Use:     CDEXPORT,
		Short:   "Export a VirtualMachine to a containerdisk.",
		Example: usage(),
		RunE: func(cmd *cobra.Command, _ []string) error {
			e.setDefaults()
			return e.run(cmd)
		},
	}

	cmd.Flags().StringVar(&e.vm, VmFlag, e.vm, "Specify the name of the VM.")
	cmd.Flags().StringVar(&e.cd, CdFlag, e.cd, "Specify the containerdisk to export the VM to.")
	cmd.Flags().StringVar(&e.name, NameFlag, e.name, "Specify the name of the exported VM.")

	err := cmd.MarkFlagRequired(VmFlag)
	if err != nil {
		panic(err)
	}
	err = cmd.MarkFlagRequired(CdFlag)
	if err != nil {
		panic(err)
	}

	cmd.SetUsageTemplate(templates.UsageTemplate())

	return cmd
}

func usage() string {
	// TODO: write me
	return `  # Export a VirtualMachine to a containerdisk`
}

func (e *exportCd) setDefaults() {
	if e.name == "" {
		e.name = e.vm + "-exported"
	}
}

func (e *exportCd) run(cmd *cobra.Command) error {
	client, err := kubecli.GetKubevirtClientFromClientConfig(e.clientConfig)
	if err != nil {
		return err
	}
	namespace, _, err := e.clientConfig.Namespace()
	if err != nil {
		return err
	}

	vm, err := client.VirtualMachine(namespace).Get(e.vm, &metav1.GetOptions{})
	if err != nil {
		return err
	}

	cmd.Printf("Exporting VM %s to temporary directory\n", vm.Name)
	tmpDir, err := os.MkdirTemp("", CDEXPORT)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := exportVM(client, tmpDir, vm); err != nil {
		return err
	}

	disks, err := os.ReadDir(tmpDir)
	if err != nil {
		return err
	}
	err = gunzipDisks(tmpDir, disks)
	if err != nil {
		return err
	}
	e.prepareVM(vm, disks)

	cmd.Println("Building containerdisk")
	img, err := buildContainerdisk(tmpDir, vm)
	if err != nil {
		return err
	}

	cmd.Printf("Pushing containerdisk %s to registry\n", e.cd)
	err = crane.Push(img, e.cd)
	if err != nil {
		return err
	}

	fileName := e.name + suffixYaml
	cmd.Printf("Writing manifest of exported VM to %s\n", fileName)
	return writeManifest(vm, fileName)
}

func (e *exportCd) prepareVM(vm *virtv1.VirtualMachine, disks []os.DirEntry) {
	for _, disk := range disks {
		e.replaceVolume(vm, disk)
	}
	e.clearVM(vm)
}

func (e *exportCd) replaceVolume(vm *virtv1.VirtualMachine, image os.DirEntry) {
	name, _, _ := strings.Cut(image.Name(), ".")
	for i, volume := range vm.Spec.Template.Spec.Volumes {
		if volume.Name == name {
			newVolume := virtv1.Volume{
				Name: name,
				VolumeSource: virtv1.VolumeSource{
					ContainerDisk: &virtv1.ContainerDiskSource{
						Image: e.cd,
						Path:  "/" + prefixPath + name + suffixImg,
					},
				},
			}
			vm.Spec.Template.Spec.Volumes[i] = newVolume
			break
		}
	}
}

func (e *exportCd) clearVM(vm *virtv1.VirtualMachine) {
	vm.ObjectMeta = metav1.ObjectMeta{Name: e.name}
	vm.Spec.DataVolumeTemplates = nil
	vm.Status = virtv1.VirtualMachineStatus{}
}

func exportVM(client kubecli.KubevirtClient, tmpDir string, vm *virtv1.VirtualMachine) error {
	vmeInfo := &vmexport.VMExportInfo{
		ShouldCreate: false,
		KeepVme:      true,
		Name:         vm.Name,
		Namespace:    vm.Namespace,
		ExportSource: k8sv1.TypedLocalObjectReference{
			APIGroup: &virtv1.SchemeGroupVersion.Group,
			Kind:     "VirtualMachine",
			Name:     vm.Name,
		},
	}

	if err := vmexport.CreateVirtualMachineExport(client, vmeInfo); err != nil {
		return err
	}

	if err := vmexport.ExportProcessingComplete(client, vmeInfo, vmexport.ProcessingWaitInterval, vmexport.ProcessingWaitTotal); err != nil {
		return err
	}

	export, err := client.VirtualMachineExport(vmeInfo.Namespace).Get(context.Background(), vmeInfo.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for _, volume := range export.Status.Links.External.Volumes {
		suffix := ""
		for _, format := range volume.Formats {
			// See order in vmexport.GetUrlFromVirtualMachineExport
			switch format.Format {
			case exportv1.KubeVirtGz:
				suffix = suffixImgGz
				break
			case exportv1.KubeVirtRaw:
				suffix = suffixImg
			}
		}
		if suffix == "" {
			continue
		}

		vmeInfo.VolumeName = volume.Name
		vmeInfo.OutputFile = filepath.Join(tmpDir, volume.Name+suffix)
		if err := vmexport.DownloadVirtualMachineExport(client, vmeInfo); err != nil {
			return err
		}
	}

	return vmexport.DeleteVirtualMachineExport(client, vmeInfo)
}

func gunzipDisk(tmpDir string, image os.DirEntry) error {
	srcPath := filepath.Join(tmpDir, image.Name())
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}

	reader, err := gzip.NewReader(src)
	if err != nil {
		return err
	}

	dst, err := os.Create(strings.TrimSuffix(srcPath, suffixImgGz) + suffixImg)
	if err != nil {
		return err
	}

	_, err = io.Copy(dst, reader)
	if err != nil {
		return err
	}

	err = dst.Close()
	if err != nil {
		return err
	}

	err = reader.Close()
	if err != nil {
		return err
	}

	err = src.Close()
	if err != nil {
		return err
	}

	return os.Remove(srcPath)
}

func gunzipDisks(tmpDir string, disks []os.DirEntry) error {
	for _, disk := range disks {
		if strings.HasSuffix(disk.Name(), suffixImgGz) {
			if err := gunzipDisk(tmpDir, disk); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildContainerdisk(tmpDir string, vm *virtv1.VirtualMachine) (v1.Image, error) {
	img := empty.Image
	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, types.OCIConfigJSON)

	layer, err := tarball.LayerFromOpener(buildDisksLayer(tmpDir), tarball.WithMediaType(types.OCILayer))
	if err != nil {
		return nil, err
	}

	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return nil, err
	}

	manifest, err := json.Marshal(vm)
	if err != nil {
		return nil, err
	}
	img, ok := mutate.Annotations(img, map[string]string{ManifestAnnotation: string(manifest)}).(v1.Image)
	if !ok {
		return nil, errors.New("failed to set annotations of the image")
	}

	// TODO: architecture is fixed to amd64 for now
	cf, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}
	cf.Architecture = imgArch
	img, err = mutate.ConfigFile(img, cf)
	if err != nil {
		return nil, err
	}

	return img, nil
}

func buildDisksLayer(tmpDir string) func() (io.ReadCloser, error) {
	modTime := time.Now()

	return func() (io.ReadCloser, error) {
		r, w := io.Pipe()

		go func() {
			defer w.Close()
			writer := tar.NewWriter(w)

			if err := writeDir(writer, modTime); err != nil {
				_ = w.CloseWithError(err)
				return
			}

			disks, err := os.ReadDir(tmpDir)
			if err != nil {
				w.CloseWithError(err)
				return
			}
			for _, disk := range disks {
				if err := writeDisk(writer, tmpDir, disk); err != nil {
					w.CloseWithError(err)
					return
				}
			}

			err = writer.Close()
			if err != nil {
				w.CloseWithError(err)
			}
		}()

		return r, nil
	}
}

func writeDir(writer *tar.Writer, modTime time.Time) error {
	header := &tar.Header{
		Typeflag: tar.TypeDir,
		Name:     prefixPath,
		Mode:     0555,
		Uid:      qemuId,
		Gid:      qemuId,
		Uname:    qemuName,
		Gname:    qemuName,
		ModTime:  modTime,
	}

	err := writer.WriteHeader(header)
	return err
}

func writeDisk(writer *tar.Writer, tmpDir string, disk os.DirEntry) error {
	f, err := os.Open(filepath.Join(tmpDir, disk.Name()))
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     prefixPath + disk.Name(),
		Mode:     0444,
		Uid:      qemuId,
		Gid:      qemuId,
		Uname:    qemuName,
		Gname:    qemuName,
		ModTime:  stat.ModTime(),
		Size:     stat.Size(),
	}

	if err := writer.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(writer, f)
	if err != nil {
		return err
	}

	return nil
}

func writeManifest(vm *virtv1.VirtualMachine, fileName string) error {
	manifest, err := yaml.Marshal(vm)
	if err != nil {
		return err
	}
	return os.WriteFile(fileName, manifest, 0666)
}
