package cluster

import (
	"fmt"
	"strings"

	"github.com/zalando/postgres-operator/pkg/spec"
	"github.com/zalando/postgres-operator/pkg/util/constants"
	"github.com/zalando/postgres-operator/pkg/util/filesystems"
)

func (c *Cluster) getPostgresFilesystemInfo(podName *spec.NamespacedName) (device, fstype string, err error) {
	out, err := c.ExecCommand(podName, "bash", "-c", fmt.Sprintf("df -T %s|tail -1", constants.PostgresDataMount))
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return "", "", fmt.Errorf("too few fields in the df output")
	}

	return fields[0], fields[1], nil
}

func (c *Cluster) resizePostgresFilesystem(podName *spec.NamespacedName, resizers []filesystems.FilesystemResizer) error {
	// resize2fs always writes to stderr, and ExecCommand considers a non-empty stderr an error
	// first, determine the device and the filesystem
	deviceName, fsType, err := c.getPostgresFilesystemInfo(podName)
	if err != nil {
		return fmt.Errorf("could not get device and type for the postgres filesystem: %v", err)
	}
	for _, resizer := range resizers {
		if !resizer.CanResizeFilesystem(fsType) {
			continue
		}
		err := resizer.ResizeFilesystem(deviceName, func(cmd string) (out string, err error) {
			return c.ExecCommand(podName, "bash", "-c", cmd)
		})

		return err
	}
	return fmt.Errorf("could not resize filesystem: no compatible resizers for the filesystem of type %q", fsType)
}
