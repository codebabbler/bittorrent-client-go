package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"unicode"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

func decodeString(input string, pos *int) (string, error) {
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

func decodeInteger(input string, pos *int) (int, error) {
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

func decodeList(input string, pos *int) ([]interface{}, error) {
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

func decodeDict(input string, pos *int) (map[string]interface{}, error) {
	(*pos)++ // skip 'd'

	result := make(map[string]interface{})

	for *pos < len(input) {
		if input[*pos] == 'e' {
			(*pos)++ // consume 'e'
			return result, nil
		}

		// Keys must always be strings in bencode
		if !unicode.IsDigit(rune(input[*pos])) {
			return nil, fmt.Errorf("invalid bencoded dict: key must be a string")
		}

		key, err := decodeString(input, pos)
		if err != nil {
			return nil, fmt.Errorf("invalid bencoded dict key: %w", err)
		}

		value, err := decode(input, pos)
		if err != nil {
			return nil, fmt.Errorf("invalid bencoded dict value for key %q: %w", key, err)
		}

		result[key] = value
	}

	return nil, fmt.Errorf("invalid bencoded dict: missing terminating 'e'")
}

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345
func decodeBencode(bencodedString string) (interface{}, error) {
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
		return decodeString(input, pos)
	case char == 'i':
		return decodeInteger(input, pos)
	case char == 'l':
		return decodeList(input, pos)
	case char == 'd':
		return decodeDict(input, pos)
	default:
		return "", fmt.Errorf("unsupported bencode type: %q", char)
	}
}

func main() {
	fmt.Fprintln(os.Stderr, "Logs from your program will appear here!")

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: ./runner.sh <command> [args]")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "decode":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: ./runner.sh decode <bencoded_value>")
			os.Exit(1)
		}

		decoded, err := decodeBencode(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}

		jsonOutput, err := json.Marshal(decoded)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error marshalling JSON:", err)
			os.Exit(1)
		}

		fmt.Println(string(jsonOutput))

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		os.Exit(1)
	}
}
