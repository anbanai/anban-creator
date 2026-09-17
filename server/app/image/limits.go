package image

import (
	"errors"
	"fmt"
	"io"
	"os"
)

const maxGeneratedImageBytes int64 = 25 << 20

var errGeneratedImageTooLarge = errors.New("generated image exceeds maximum size")

func readGeneratedImageFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxGeneratedImageBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxGeneratedImageBytes {
		return nil, fmt.Errorf("%w: limit is %d bytes", errGeneratedImageTooLarge, maxGeneratedImageBytes)
	}
	return data, nil
}
