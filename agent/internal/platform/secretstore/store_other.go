//go:build !windows

package secretstore

func Supported() bool { return false }

func Write(path string, record Record) error {
	return ErrUnsupported
}

func Read(path string) (Record, error) {
	return Record{}, ErrUnsupported
}

func Remove(path string) error {
	return ErrUnsupported
}
