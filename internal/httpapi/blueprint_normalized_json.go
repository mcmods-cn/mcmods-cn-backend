package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
)

var errBlueprintNormalizedSizeLimit = errors.New("normalized blueprint exceeds processing size limit")

type blueprintJSONBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (writer *blueprintJSONBuffer) Write(payload []byte) (int, error) {
	if len(payload) > writer.limit-writer.buffer.Len() {
		return 0, errBlueprintNormalizedSizeLimit
	}
	return writer.buffer.Write(payload)
}

func (writer *blueprintJSONBuffer) writeLiteral(value string) error {
	_, err := writer.Write([]byte(value))
	return err
}

func (writer *blueprintJSONBuffer) writeValue(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}

func encodeNormalizedBlueprintJSON(document blueprintDocument) ([]byte, error) {
	writer := &blueprintJSONBuffer{limit: maxBlueprintNormalizedBytes}
	if err := writer.writeLiteral(`{"schemaVersion":`); err != nil {
		return nil, err
	}
	if err := writer.writeValue(document.SchemaVersion); err != nil {
		return nil, err
	}
	fields := []struct {
		name    string
		value   any
		include bool
	}{
		{name: "name", value: document.Name, include: true},
		{name: "sourceFormat", value: document.SourceFormat, include: true},
		{name: "dataVersion", value: document.DataVersion, include: document.DataVersion != 0},
		{name: "size", value: document.Size, include: true},
	}
	for _, field := range fields {
		if !field.include {
			continue
		}
		if err := writer.writeLiteral(`,"` + field.name + `":`); err != nil {
			return nil, err
		}
		if err := writer.writeValue(field.value); err != nil {
			return nil, err
		}
	}
	if err := writeBlueprintJSONArray(writer, "blocks", len(document.Blocks), func(index int) any { return document.Blocks[index] }); err != nil {
		return nil, err
	}
	if len(document.BlockEntities) != 0 {
		if err := writeBlueprintJSONArray(writer, "blockEntities", len(document.BlockEntities), func(index int) any { return document.BlockEntities[index] }); err != nil {
			return nil, err
		}
	}
	if len(document.Entities) != 0 {
		if err := writeBlueprintJSONArray(writer, "entities", len(document.Entities), func(index int) any { return document.Entities[index] }); err != nil {
			return nil, err
		}
	}
	if len(document.Warnings) != 0 {
		if err := writeBlueprintJSONArray(writer, "warnings", len(document.Warnings), func(index int) any { return document.Warnings[index] }); err != nil {
			return nil, err
		}
	}
	if err := writer.writeLiteral("}"); err != nil {
		return nil, err
	}
	return writer.buffer.Bytes(), nil
}

func writeBlueprintJSONArray(writer *blueprintJSONBuffer, name string, length int, value func(int) any) error {
	if err := writer.writeLiteral(`,"` + name + `":[`); err != nil {
		return err
	}
	for index := 0; index < length; index++ {
		if index != 0 {
			if err := writer.writeLiteral(","); err != nil {
				return err
			}
		}
		if err := writer.writeValue(value(index)); err != nil {
			return err
		}
	}
	return writer.writeLiteral("]")
}
