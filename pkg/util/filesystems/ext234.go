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
	ext2      = "ext2"
	ext3      = "ext3"
	ext4      = "ext4"
	resize2fs = "resize2fs"
)

//Ext234Resize implements the FilesystemResizer interface for the ext4/3/2fs.
type Ext234Resize struct {
}

// CanResizeFilesystem checks whether Ext234Resize can resize this filesystem.
func (c *Ext234Resize) CanResizeFilesystem(fstype string) bool {
	return fstype == ext2 || fstype == ext3 || fstype == ext4
}

// ResizeFilesystem calls resize2fs to resize the filesystem if necessary.
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
	return fmt.Errorf("unrecognized output: %q, assuming error", out)
}
