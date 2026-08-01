package serverprobe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

const fabricDirectRegistryChannel = "fabric:registry/sync/direct"

func parseFabricRegistrySync(data []byte) ([]string, []string, error) {
	if len(data) == 0 {
		return nil, nil, errors.New("empty Fabric registry sync")
	}

	reader := bytes.NewReader(data)
	namespaceGroupCount, err := readBoundedConfigurationVarInt(reader, "registry namespace group", 4096)
	if err != nil {
		return nil, nil, err
	}

	var registryNames []string
	var entries []string
	for groupIndex := int32(0); groupIndex < namespaceGroupCount; groupIndex++ {
		registryNamespace, err := readProtocolString(reader, maxConfigurationPacketBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("registry namespace group %d: %w", groupIndex, err)
		}
		registryNamespace = unoptimizeFabricNamespace(registryNamespace)

		registryCount, err := readBoundedConfigurationVarInt(reader, "registry", 1_000_000)
		if err != nil {
			return nil, nil, fmt.Errorf("registry namespace %q: %w", registryNamespace, err)
		}
		for registryIndex := int32(0); registryIndex < registryCount; registryIndex++ {
			registryPath, err := readProtocolString(reader, maxConfigurationPacketBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("registry %s #%d path: %w", registryNamespace, registryIndex, err)
			}
			registryNames = append(registryNames, registryNamespace+":"+registryPath)

			entryNamespaceCount, err := readBoundedConfigurationVarInt(reader, "entry namespace", 1_000_000)
			if err != nil {
				return nil, nil, fmt.Errorf("registry %s:%s: %w", registryNamespace, registryPath, err)
			}
			for entryNamespaceIndex := int32(0); entryNamespaceIndex < entryNamespaceCount; entryNamespaceIndex++ {
				entryNamespace, err := readProtocolString(reader, maxConfigurationPacketBytes)
				if err != nil {
					return nil, nil, fmt.Errorf(
						"registry %s:%s entry namespace %d: %w",
						registryNamespace,
						registryPath,
						entryNamespaceIndex,
						err,
					)
				}
				entryNamespace = unoptimizeFabricNamespace(entryNamespace)

				bulkCount, err := readBoundedConfigurationVarInt(reader, "raw id bulk", 1_000_000)
				if err != nil {
					return nil, nil, fmt.Errorf(
						"registry %s:%s namespace %s: %w",
						registryNamespace,
						registryPath,
						entryNamespace,
						err,
					)
				}
				for bulkIndex := int32(0); bulkIndex < bulkCount; bulkIndex++ {
					if _, err = readVarInt(reader); err != nil {
						return nil, nil, fmt.Errorf(
							"registry %s:%s namespace %s bulk %d raw id delta: %w",
							registryNamespace,
							registryPath,
							entryNamespace,
							bulkIndex,
							err,
						)
					}
					bulkSize, err := readBoundedConfigurationVarInt(reader, "raw id bulk entry", 1_000_000)
					if err != nil {
						return nil, nil, fmt.Errorf(
							"registry %s:%s namespace %s bulk %d: %w",
							registryNamespace,
							registryPath,
							entryNamespace,
							bulkIndex,
							err,
						)
					}
					if int64(len(entries))+int64(bulkSize) > 5_000_000 {
						return nil, nil, errors.New("Fabric registry sync contains more than 5000000 entries")
					}
					for entryIndex := int32(0); entryIndex < bulkSize; entryIndex++ {
						entryPath, err := readProtocolString(reader, maxConfigurationPacketBytes)
						if err != nil {
							return nil, nil, fmt.Errorf(
								"registry %s:%s namespace %s bulk %d entry %d: %w",
								registryNamespace,
								registryPath,
								entryNamespace,
								bulkIndex,
								entryIndex,
								err,
							)
						}
						entries = append(entries, entryNamespace+":"+entryPath)
					}
				}
			}
		}
	}
	if reader.Len() != 0 {
		padding := make([]byte, reader.Len())
		_, _ = io.ReadFull(reader, padding)
		for _, value := range padding {
			if value != 0 {
				return nil, nil, fmt.Errorf("%d non-padding trailing bytes in Fabric registry sync", len(padding))
			}
		}
	}
	return registryNames, entries, nil
}

func readBoundedConfigurationVarInt(reader io.ByteReader, label string, maximum int32) (int32, error) {
	value, err := readVarInt(reader)
	if err != nil {
		return 0, err
	}
	if value < 0 || value > maximum {
		return 0, fmt.Errorf("invalid %s count %d", label, value)
	}
	return value, nil
}

func unoptimizeFabricNamespace(namespace string) string {
	if namespace == "" {
		return "minecraft"
	}
	return namespace
}
