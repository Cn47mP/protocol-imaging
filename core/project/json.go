package project

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func decodeExtensible[T any](data []byte, knownFields ...string) (T, ExtraFields, error) {
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return value, nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return value, nil, err
	}
	if raw == nil {
		return value, nil, fmt.Errorf("expected a JSON object")
	}
	for _, field := range knownFields {
		delete(raw, field)
	}
	if len(raw) == 0 {
		return value, nil, nil
	}

	extra := make(ExtraFields, len(raw))
	for key, message := range raw {
		extra[key] = bytes.Clone(message)
	}
	return value, extra, nil
}

func encodeExtensible(value any, extra ExtraFields) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return data, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	for key, message := range extra {
		if _, known := object[key]; known {
			continue
		}
		if !json.Valid(message) {
			return nil, fmt.Errorf("extra field %q contains invalid JSON", key)
		}
		object[key] = bytes.Clone(message)
	}
	return json.Marshal(object)
}

func (value *Manifest) UnmarshalJSON(data []byte) error {
	type plain Manifest
	decoded, extra, err := decodeExtensible[plain](data,
		"format", "format_version", "project_id", "title", "created_at", "updated_at",
		"game_version", "profile", "geometry", "coordinate_system", "capture_state",
		"capture", "active_calibration", "active_plan", "active_version", "layers", "annotations",
	)
	if err != nil {
		return err
	}
	*value = Manifest(decoded)
	value.Extra = extra
	return nil
}

func (value Manifest) MarshalJSON() ([]byte, error) {
	type plain Manifest
	return encodeExtensible(plain(value), value.Extra)
}

func (value *ProfileReference) UnmarshalJSON(data []byte) error {
	type plain ProfileReference
	decoded, extra, err := decodeExtensible[plain](data, "id", "snapshot")
	if err != nil {
		return err
	}
	*value = ProfileReference(decoded)
	value.Extra = extra
	return nil
}

func (value ProfileReference) MarshalJSON() ([]byte, error) {
	type plain ProfileReference
	return encodeExtensible(plain(value), value.Extra)
}

func (value *GeometryDescriptor) UnmarshalJSON(data []byte) error {
	type plain GeometryDescriptor
	decoded, extra, err := decodeExtensible[plain](data, "status", "source", "observation", "coordinate_compatibility")
	if err != nil {
		return err
	}
	*value = GeometryDescriptor(decoded)
	value.Extra = extra
	return nil
}

func (value GeometryDescriptor) MarshalJSON() ([]byte, error) {
	type plain GeometryDescriptor
	return encodeExtensible(plain(value), value.Extra)
}

func (value *CoordinateSystem) UnmarshalJSON(data []byte) error {
	type plain CoordinateSystem
	decoded, extra, err := decodeExtensible[plain](data, "space_id", "unit", "axis")
	if err != nil {
		return err
	}
	*value = CoordinateSystem(decoded)
	value.Extra = extra
	return nil
}

func (value CoordinateSystem) MarshalJSON() ([]byte, error) {
	type plain CoordinateSystem
	return encodeExtensible(plain(value), value.Extra)
}

func (value *LayerReference) UnmarshalJSON(data []byte) error {
	type plain LayerReference
	decoded, extra, err := decodeExtensible[plain](data, "id", "path")
	if err != nil {
		return err
	}
	*value = LayerReference(decoded)
	value.Extra = extra
	return nil
}

func (value LayerReference) MarshalJSON() ([]byte, error) {
	type plain LayerReference
	return encodeExtensible(plain(value), value.Extra)
}

func (value *CaptureDocument) UnmarshalJSON(data []byte) error {
	type plain CaptureDocument
	decoded, extra, err := decodeExtensible[plain](data, "schema_version", "request", "environment", "limits", "calibrations")
	if err != nil {
		return err
	}
	*value = CaptureDocument(decoded)
	value.Extra = extra
	return nil
}

func (value CaptureDocument) MarshalJSON() ([]byte, error) {
	type plain CaptureDocument
	return encodeExtensible(plain(value), value.Extra)
}

func (value *CaptureRequest) UnmarshalJSON(data []byte) error {
	type plain CaptureRequest
	decoded, extra, err := decodeExtensible[plain](data,
		"version", "frozen_at", "imaging_mode", "quality_level", "target_overlap",
		"burst_policy", "diagnostics", "generate_panorama", "generate_pyramid",
	)
	if err != nil {
		return err
	}
	*value = CaptureRequest(decoded)
	value.Extra = extra
	return nil
}

func (value CaptureRequest) MarshalJSON() ([]byte, error) {
	type plain CaptureRequest
	return encodeExtensible(plain(value), value.Extra)
}

func (value *Overlap) UnmarshalJSON(data []byte) error {
	type plain Overlap
	decoded, extra, err := decodeExtensible[plain](data, "horizontal", "vertical")
	if err != nil {
		return err
	}
	*value = Overlap(decoded)
	value.Extra = extra
	return nil
}

func (value Overlap) MarshalJSON() ([]byte, error) {
	type plain Overlap
	return encodeExtensible(plain(value), value.Extra)
}

func (value *EnvironmentFingerprint) UnmarshalJSON(data []byte) error {
	type plain EnvironmentFingerprint
	decoded, extra, err := decodeExtensible[plain](data,
		"observed_at", "controller_kind", "raw_frame_size", "input_viewport",
		"effective_crop", "dpi_scale", "window", "game_version",
	)
	if err != nil {
		return err
	}
	*value = EnvironmentFingerprint(decoded)
	value.Extra = extra
	return nil
}

func (value EnvironmentFingerprint) MarshalJSON() ([]byte, error) {
	type plain EnvironmentFingerprint
	return encodeExtensible(plain(value), value.Extra)
}

func (value *PixelDimensions) UnmarshalJSON(data []byte) error {
	type plain PixelDimensions
	decoded, extra, err := decodeExtensible[plain](data, "width", "height")
	if err != nil {
		return err
	}
	*value = PixelDimensions(decoded)
	value.Extra = extra
	return nil
}

func (value PixelDimensions) MarshalJSON() ([]byte, error) {
	type plain PixelDimensions
	return encodeExtensible(plain(value), value.Extra)
}

func (value *WindowFingerprint) UnmarshalJSON(data []byte) error {
	type plain WindowFingerprint
	decoded, extra, err := decodeExtensible[plain](data, "process_name", "class_name", "title_hash", "client_size")
	if err != nil {
		return err
	}
	*value = WindowFingerprint(decoded)
	value.Extra = extra
	return nil
}

func (value WindowFingerprint) MarshalJSON() ([]byte, error) {
	type plain WindowFingerprint
	return encodeExtensible(plain(value), value.Extra)
}

func (value *CalibrationRecord) UnmarshalJSON(data []byte) error {
	type plain CalibrationRecord
	decoded, extra, err := decodeExtensible[plain](data,
		"id", "created_at", "actions", "horizontal_motion", "vertical_motion",
		"effective_viewport", "input_to_raw", "raw_to_session", "confidence",
		"invalidated_at", "invalidation_reason",
	)
	if err != nil {
		return err
	}
	*value = CalibrationRecord(decoded)
	value.Extra = extra
	return nil
}

func (value CalibrationRecord) MarshalJSON() ([]byte, error) {
	type plain CalibrationRecord
	return encodeExtensible(plain(value), value.Extra)
}

func (value *CalibrationAction) UnmarshalJSON(data []byte) error {
	type plain CalibrationAction
	decoded, extra, err := decodeExtensible[plain](data, "purpose", "input_delta", "measured_raw_delta", "evidence_ids")
	if err != nil {
		return err
	}
	*value = CalibrationAction(decoded)
	value.Extra = extra
	return nil
}

func (value CalibrationAction) MarshalJSON() ([]byte, error) {
	type plain CalibrationAction
	return encodeExtensible(plain(value), value.Extra)
}

func (value *PlanDocument) UnmarshalJSON(data []byte) error {
	type plain PlanDocument
	decoded, extra, err := decodeExtensible[plain](data,
		"schema_version", "id", "created_at", "supersedes", "trigger", "frontier",
		"required_adjacencies", "acceptable_overlap", "coverage_audit",
	)
	if err != nil {
		return err
	}
	*value = PlanDocument(decoded)
	value.Extra = extra
	return nil
}

func (value PlanDocument) MarshalJSON() ([]byte, error) {
	type plain PlanDocument
	return encodeExtensible(plain(value), value.Extra)
}

func (value *TileAdjacency) UnmarshalJSON(data []byte) error {
	type plain TileAdjacency
	decoded, extra, err := decodeExtensible[plain](data, "from_tile", "to_tile", "axis")
	if err != nil {
		return err
	}
	*value = TileAdjacency(decoded)
	value.Extra = extra
	return nil
}

func (value TileAdjacency) MarshalJSON() ([]byte, error) {
	type plain TileAdjacency
	return encodeExtensible(plain(value), value.Extra)
}

func (value *OverlapRange) UnmarshalJSON(data []byte) error {
	type plain OverlapRange
	decoded, extra, err := decodeExtensible[plain](data, "minimum", "maximum")
	if err != nil {
		return err
	}
	*value = OverlapRange(decoded)
	value.Extra = extra
	return nil
}

func (value OverlapRange) MarshalJSON() ([]byte, error) {
	type plain OverlapRange
	return encodeExtensible(plain(value), value.Extra)
}

func (value *CoverageAudit) UnmarshalJSON(data []byte) error {
	type plain CoverageAudit
	decoded, extra, err := decodeExtensible[plain](data,
		"algorithm", "audited_at", "passed", "covered_tile_ids", "missing_tile_ids",
		"verified_adjacencies", "confidence",
	)
	if err != nil {
		return err
	}
	*value = CoverageAudit(decoded)
	value.Extra = extra
	return nil
}

func (value CoverageAudit) MarshalJSON() ([]byte, error) {
	type plain CoverageAudit
	return encodeExtensible(plain(value), value.Extra)
}

func (value *BoundaryDocument) UnmarshalJSON(data []byte) error {
	type plain BoundaryDocument
	decoded, extra, err := decodeExtensible[plain](data,
		"schema_version", "revision", "status", "coordinate_compatibility", "origin",
		"confirmed_edges", "events", "rows", "bounds", "current_position", "travel",
		"closure_error", "confidence", "unresolved_reason",
	)
	if err != nil {
		return err
	}
	*value = BoundaryDocument(decoded)
	value.Extra = extra
	return nil
}

func (value BoundaryDocument) MarshalJSON() ([]byte, error) {
	type plain BoundaryDocument
	return encodeExtensible(plain(value), value.Extra)
}

func (value *BoundaryEvent) UnmarshalJSON(data []byte) error {
	type plain BoundaryEvent
	decoded, extra, err := decodeExtensible[plain](data,
		"sequence", "phase", "action_id", "observed_at", "intent", "observation",
		"confirmed_edge", "source_frame_ids",
	)
	if err != nil {
		return err
	}
	*value = BoundaryEvent(decoded)
	value.Extra = extra
	return nil
}

func (value BoundaryEvent) MarshalJSON() ([]byte, error) {
	type plain BoundaryEvent
	return encodeExtensible(plain(value), value.Extra)
}

func (value *BoundaryRow) UnmarshalJSON(data []byte) error {
	type plain BoundaryRow
	decoded, extra, err := decodeExtensible[plain](data,
		"index", "direction", "min_x", "max_x", "min_y", "max_y", "tile_ids", "end_confirmed",
	)
	if err != nil {
		return err
	}
	*value = BoundaryRow(decoded)
	value.Extra = extra
	return nil
}

func (value BoundaryRow) MarshalJSON() ([]byte, error) {
	type plain BoundaryRow
	return encodeExtensible(plain(value), value.Extra)
}

func (value *ObservedBounds) UnmarshalJSON(data []byte) error {
	type plain ObservedBounds
	decoded, extra, err := decodeExtensible[plain](data, "min_x", "max_x", "min_y", "max_y")
	if err != nil {
		return err
	}
	*value = ObservedBounds(decoded)
	value.Extra = extra
	return nil
}

func (value ObservedBounds) MarshalJSON() ([]byte, error) {
	type plain ObservedBounds
	return encodeExtensible(plain(value), value.Extra)
}
