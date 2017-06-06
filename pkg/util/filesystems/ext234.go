package filesystems

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	ext2fsSuccessRegexp = regexp.MustCompile(`The filesystem on [/a-z0-9]+ is now \d+ \(\d+\w+\) blocks long.`)
)

const (
	EXT2      = "ext2"
	EXT3      = "ext3"
	EXT4      = "ext4"
	resize2fs = "resize2fs"
)

type Ext234Resize struct {
}

func (c *Ext234Resize) CanResizeFilesystem(fstype string) bool {
	return fstype == EXT2 || fstype == EXT3 || fstype == EXT4
}

func (c *Ext234Resize) ResizeFilesystem(deviceName string, commandExecutor func(cmd string) (out string, err error)) error {
	command := fmt.Sprintf("%s %s 2>&1", resize2fs, deviceName)
	out, err := commandExecutor(command)
	if err != nil {
		return err
	}
	if strings.Contains(out, "Nothing to do") ||
		(strings.Contains(out, "on-line resizing required") && ext2fsSuccessRegexp.MatchString(out)) {
		return nil
	}
	return fmt.Errorf("unrecognized output: %s, assuming error", out)
}
