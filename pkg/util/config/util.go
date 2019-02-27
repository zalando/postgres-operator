package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type decoder interface {
	Decode(value string) error
}

type fieldInfo struct {
	Name    string
	Default string
	Field   reflect.Value
}

// StringTemplate is a convenience alias
type StringTemplate string

func decoderFrom(field reflect.Value) (d decoder) {
	// it may be impossible for a struct field to fail this check
	if !field.CanInterface() {
		return
	}

	d, ok := field.Interface().(decoder)
	if !ok && field.CanAddr() {
		d, _ = field.Addr().Interface().(decoder)
	}

	return d
}

// taken from github.com/kelseyhightower/envconfig
func structFields(spec interface{}) ([]fieldInfo, error) {
	s := reflect.ValueOf(spec).Elem()

	// over allocate an info array, we will extend if needed later
	infos := make([]fieldInfo, 0, s.NumField())
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		ftype := s.Type().Field(i)

		fieldName := ftype.Tag.Get("name")
		if fieldName == "" {
			fieldName = strings.ToLower(ftype.Name)
		}

		// Capture information about the config variable
		info := fieldInfo{
			Name:    fieldName,
			Field:   f,
			Default: ftype.Tag.Get("default"),
		}
		infos = append(infos, info)

		if f.Kind() == reflect.Struct {
			// honor Decode if present
			if decoderFrom(f) == nil {
				embeddedPtr := f.Addr().Interface()
				embeddedInfos, err := structFields(embeddedPtr)
				if err != nil {
					return nil, err
				}
				infos = append(infos[:len(infos)-1], embeddedInfos...)

				continue
			}
		}
	}

	return infos, nil
}

func processField(value string, field reflect.Value) error {
	typ := field.Type()

	decoder := decoderFrom(field)
	if decoder != nil {
		return decoder.Decode(value)
	}

	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
		if field.IsNil() {
			field.Set(reflect.New(typ))
		}
		field = field.Elem()
	}

	switch typ.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var (
			val int64
			err error
		)
		if field.Kind() == reflect.Int64 && typ.PkgPath() == "time" && typ.Name() == "Duration" {
			var d time.Duration
			d, err = time.ParseDuration(value)
			val = int64(d)
		} else {
			val, err = strconv.ParseInt(value, 0, typ.Bits())
		}
		if err != nil {
			return err
		}

		field.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(value, 0, typ.Bits())
		if err != nil {
			return err
		}
		field.SetUint(val)
	case reflect.Bool:
		val, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(val)
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(value, typ.Bits())
		if err != nil {
			return err
		}
		field.SetFloat(val)
	case reflect.Slice:
		vals := strings.Split(value, ",")
		sl := reflect.MakeSlice(typ, len(vals), len(vals))
		for i, val := range vals {
			err := processField(val, sl.Index(i))
			if err != nil {
				return err
			}
		}
		field.Set(sl)
	case reflect.Map:
		pairs, err := getMapPairsFromString(value)
		if err != nil {
			return fmt.Errorf("could not split value %q into map items: %v", value, err)
		}
		mp := reflect.MakeMap(typ)
		for _, pair := range pairs {
			kvpair := strings.Split(pair, ":")
			if len(kvpair) != 2 {
				return fmt.Errorf("invalid map item: %q", pair)
			}
			k := reflect.New(typ.Key()).Elem()
			err := processField(kvpair[0], k)
			if err != nil {
				return err
			}
			v := reflect.New(typ.Elem()).Elem()
			err = processField(kvpair[1], v)
			if err != nil {
				return err
			}
			mp.SetMapIndex(k, v)
		}
		field.Set(mp)
	}

	return nil
}

type parserState int

const (
	plain parserState = iota
	doubleQuoted
	singleQuoted
)

// Split the pair candidates by commas not located inside open quotes
// Escape characters are not supported for simplicity, as we don't
// expect to find them inside the map values for our use cases
func getMapPairsFromString(value string) (pairs []string, err error) {
	pairs = make([]string, 0)
	state := plain
	var start, quote int

	for i, ch := range strings.Split(value, "") {
		if (ch == `"` || ch == `'`) && i > 0 && value[i-1] == '\\' {
			fmt.Printf("Parser warning: ecape character '\\' have no effect on quotes inside the configuration value %s\n", value)
		}
		if ch == `"` {
			if state == plain {
				state = doubleQuoted
				quote = i
			} else if state == doubleQuoted {
				state = plain
				quote = 0
			}
		}
		if ch == "'" {
			if state == plain {
				state = singleQuoted
				quote = i
			} else if state == singleQuoted {
				state = plain
				quote = 0
			}
		}
		if ch == "," && state == plain {
			pairs = append(pairs, strings.Trim(value[start:i], " \t"))
			start = i + 1
		}
	}
	if state != plain {
		err = fmt.Errorf("unmatched quote starting at position %d", quote+1)
		pairs = nil
	} else {
		pairs = append(pairs, strings.Trim(value[start:], " \t"))
	}
	return
}

// Decode cast value to StringTemplate
func (f *StringTemplate) Decode(value string) error {
	*f = StringTemplate(value)

	return nil
}

// Format formatted string from StringTemplate
func (f *StringTemplate) Format(a ...string) string {
	res := string(*f)

	for i := 0; i < len(a); i += 2 {
		res = strings.Replace(res, "{"+a[i]+"}", a[i+1], -1)
	}

	return res
}

// MarshalJSON converts a StringTemplate to byte slice
func (f StringTemplate) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(f))
}
