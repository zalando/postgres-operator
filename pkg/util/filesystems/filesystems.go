package filesystems

type FilesystemResizer interface {
	CanResizeFilesystem(fstype string) bool
	ResizeFilesystem(deviceName string, commandExecutor func(string) (out string, err error)) error
}
