package bencode

import (
	"fmt"
	"strconv"
	"unicode"
)

func DecodeString(input string, pos *int) (string, error) {
	start := *pos

	for *pos < len(input) && input[*pos] != ':' {
		if !unicode.IsDigit(rune(input[*pos])) {
			return "", fmt.Errorf("invalid bencoded string length")
		}
		(*pos)++
	}

	if *pos >= len(input) {
		return "", fmt.Errorf("invalid bencoded string: missing colon")
	}

	lengthStr := input[start:*pos]
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", err
	}

	(*pos)++ // skip ':'

	if *pos+length > len(input) {
		return "", fmt.Errorf("invalid bencoded string: length exceeds data")
	}

	result := input[*pos : *pos+length]
	*pos += length

	return result, nil
}

func DecodeInteger(input string, pos *int) (int, error) {
	(*pos)++ // skip 'i'
	start := *pos

	for *pos < len(input) && input[*pos] != 'e' {
		(*pos)++
	}

	if *pos >= len(input) {
		return 0, fmt.Errorf("Invalid bencoded integer")
	}

	number, err := strconv.Atoi(input[start:*pos])
	if err != nil {
		return 0, err
	}

	(*pos)++ // skip 'e'
	return number, nil
}

func DecodeList(input string, pos *int) ([]interface{}, error) {
	(*pos)++ // skip 'l'

	result := []interface{}{}

	for *pos < len(input) {
		if input[*pos] == 'e' {
			(*pos)++ // consume 'e'
			return result, nil
		}

		value, err := decode(input, pos)
		if err != nil {
			return nil, err
		}

		result = append(result, value)
	}

	return nil, fmt.Errorf("invalid bencoded list: missing terminating 'e'")
}

// DecodeDict returns the parsed map and a map of raw bencoded bytes per key
func DecodeDict(input string, pos *int) (map[string]interface{}, map[string]string, error) {
	(*pos)++ // skip 'd'

	result := make(map[string]interface{})
	rawValues := make(map[string]string)

	for *pos < len(input) {
		if input[*pos] == 'e' {
			(*pos)++ // consume 'e'
			return result, rawValues, nil
		}

		// Keys must always be strings in bencode
		if !unicode.IsDigit(rune(input[*pos])) {
			return nil, nil, fmt.Errorf("invalid bencoded dict: key must be a string")
		}

		key, err := DecodeString(input, pos)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid bencoded dict key: %w", err)
		}

		startPos := *pos
		value, err := decode(input, pos)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid bencoded dict value for key %q: %w", key, err)
		}

		result[key] = value
		rawValues[key] = input[startPos:*pos]
	}

	return nil, nil, fmt.Errorf("invalid bencoded dict: missing terminating 'e'")
}

// Decode decodes a bencoded string into a Go value.
func Decode(bencodedString string) (interface{}, error) {
	if len(bencodedString) == 0 {
		return "", fmt.Errorf("empty bencoded string")
	}

	pos := 0
	return decode(bencodedString, &pos)
}

func decode(input string, pos *int) (interface{}, error) {
	if *pos >= len(input) {
		return nil, fmt.Errorf("unexpected end of input")
	}

	switch char := input[*pos]; {
	case unicode.IsDigit(rune(char)):
		return DecodeString(input, pos)
	case char == 'i':
		return DecodeInteger(input, pos)
	case char == 'l':
		return DecodeList(input, pos)
	case char == 'd':
		dict, _, err := DecodeDict(input, pos)
		return dict, err
	default:
		return "", fmt.Errorf("unsupported bencode type: %q", char)
	}
}
