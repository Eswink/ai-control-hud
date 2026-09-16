//go:build !windows && !linux && !darwin

package secretstore

func Supported() bool { return false }

func Write(path string, record Record) error {
	return ErrUnsupported
}

func Read(path string) (Record, error) {
	return Record{}, ErrUnsupported
}

func WriteHub(path string, record HubRecord) error {
	return ErrUnsupported
}

func ReadHub(path string) (HubRecord, error) {
	return HubRecord{}, ErrUnsupported
}

func Remove(path string) error {
	return ErrUnsupported
}
