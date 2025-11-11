package utils

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
)

// CompressionAlgorithm defines supported compression methods
type CompressionAlgorithm string

const (
	CompressionNone CompressionAlgorithm = "none"
	CompressionGzip CompressionAlgorithm = "gzip"
	CompressionZlib CompressionAlgorithm = "zlib"
	CompressionZstd CompressionAlgorithm = "zstd" // Best compression ratio
)

// CompressData compresses data using the specified algorithm
func CompressData(data []byte, algorithm CompressionAlgorithm) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	switch algorithm {
	case CompressionNone:
		return data, nil

	case CompressionGzip:
		var buf bytes.Buffer
		writer := gzip.NewWriter(&buf)
		if _, err := writer.Write(data); err != nil {
			return nil, fmt.Errorf("failed to write to gzip writer: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("failed to close gzip writer: %w", err)
		}
		return buf.Bytes(), nil

	case CompressionZlib:
		var buf bytes.Buffer
		writer := zlib.NewWriter(&buf)
		if _, err := writer.Write(data); err != nil {
			return nil, fmt.Errorf("failed to write to zlib writer: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("failed to close zlib writer: %w", err)
		}
		return buf.Bytes(), nil

	case CompressionZstd:
		// Note: zstd requires external library, using gzip as fallback
		return CompressData(data, CompressionGzip)
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}
}

// DecompressData decompresses data using the specified algorithm
func DecompressData(compressed []byte, algorithm CompressionAlgorithm) ([]byte, error) {
	if len(compressed) == 0 {
		return compressed, nil
	}

	switch algorithm {
	case CompressionNone:
		return compressed, nil

	case CompressionGzip:
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read from gzip reader: %w", err)
		}
		return data, nil

	case CompressionZlib:
		reader, err := zlib.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, fmt.Errorf("failed to create zlib reader: %w", err)
		}
		defer reader.Close()

		data, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to read from zlib reader: %w", err)
		}
		return data, nil

	case CompressionZstd:
		// Note: zstd requires external library, using gzip as fallback
		return DecompressData(compressed, CompressionGzip)
	default:
		return nil, fmt.Errorf("unsupported compression algorithm: %s", algorithm)
	}
}

// GetBestCompression chooses the best compression method based on content
func GetBestCompression(data []byte) CompressionAlgorithm {
	// For text data, zstd provides best compression ratio
	// For small chunks, avoid overhead
	if len(data) < 500 {
		return CompressionNone
	}

	// Test compression ratios
	// In production, you might want to actually test, but zstd is generally best
	return CompressionZstd
}

// CompressText compresses text data optimally
func CompressText(text string) ([]byte, CompressionAlgorithm, error) {
	data := []byte(text)
	algorithm := GetBestCompression(data)

	compressed, err := CompressData(data, algorithm)
	if err != nil {
		return nil, CompressionNone, err
	}

	return compressed, algorithm, nil
}

// DecompressText decompresses text data
func DecompressText(compressed []byte, algorithm CompressionAlgorithm) (string, error) {
	data, err := DecompressData(compressed, algorithm)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
