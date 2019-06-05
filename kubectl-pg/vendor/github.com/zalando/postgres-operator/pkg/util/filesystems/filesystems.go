package filesystems

// FilesystemResizer has methods to work with resizing of a filesystem
type FilesystemResizer interface {
	CanResizeFilesystem(fstype string) bool
	ResizeFilesystem(deviceName string, commandExecutor func(string) (out string, err error)) error
}
