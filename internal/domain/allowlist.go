package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// PickerAttribute names one analysis value the fix picker may receive.
type PickerAttribute string

// The allowlist is face shape, hair type, hair length, and colour tones.
const (
	AttrFaceShape   PickerAttribute = "face shape"
	AttrHairType    PickerAttribute = "hair type"
	AttrHairLength  PickerAttribute = "hair length"
	AttrColourTones PickerAttribute = "colour tones"
)

// Allowlist is the only analysis the fix picker may receive.
// The fields are the whole set. There is no photo, so a caller cannot
// pass one in this value, and JSON with any other field is refused.
type Allowlist struct {
	FaceShape   string `json:"face_shape"`
	HairType    string `json:"hair_type"`
	HairLength  string `json:"hair_length"`
	ColourTones string `json:"colour_tones"`
}

// UnmarshalJSON refuses any field that is not on the allowlist.
func (a *Allowlist) UnmarshalJSON(data []byte) error {
	if a == nil {
		return errors.New("nil allowlist")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	type plain Allowlist
	var decoded plain
	if err := dec.Decode(&decoded); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("trailing data")
		}
		return err
	}
	*a = Allowlist(decoded)
	return nil
}

// youcamField maps a YouCam response field onto an allowlisted attribute.
// Age, gender, ratios, and any other response field are absent on purpose:
// the picker must not receive them. Hair type, hair length, and colour
// tones are on the allowlist, but no menu task returns them yet.
var youcamField = map[string]PickerAttribute{
	"faceShape": AttrFaceShape,
}

func pickerAttributeForYouCamField(field string) (PickerAttribute, bool) {
	attr, ok := youcamField[field]
	return attr, ok
}
