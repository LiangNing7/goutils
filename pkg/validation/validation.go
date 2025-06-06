package validation

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/klog/v2"
)

// Validator implements the validate.IValidator interface.
type Validator struct {
	registry map[string]reflect.Value
}

// ProviderSet is the validator providers.
// var ProviderSet = wire.NewSet(NewValidator)

// NewValidator creates and initializes a custom validator.
func NewValidator(customValidator any) *Validator {
	return &Validator{registry: extractValidationMethods(customValidator)}
}

// Validate validates the request using the appropriate validation method.
func (v *Validator) Validate(ctx context.Context, request any) error {
	validationFunc, ok := v.registry[reflect.TypeOf(request).Elem().Name()]
	if !ok {
		return nil // No validation function found for the request type
	}

	result := validationFunc.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(request)})
	if !result[0].IsNil() {
		return result[0].Interface().(error)
	}

	return nil
}

// extractValidationMethods extracts and returns a map of validation functions
// from the provided custom validator.
func extractValidationMethods(customValidator any) map[string]reflect.Value {
	funcs := make(map[string]reflect.Value)
	validatorType := reflect.TypeOf(customValidator)
	validatorValue := reflect.ValueOf(customValidator)

	for i := range validatorType.NumMethod() {
		method := validatorType.Method(i)
		methodValue := validatorValue.MethodByName(method.Name)

		if !methodValue.IsValid() || !strings.HasPrefix(method.Name, "Validate") {
			continue
		}

		methodType := methodValue.Type()

		// Ensure the method takes a context.Context and a pointer
		if methodType.NumIn() != 2 || methodType.NumOut() != 1 ||
			methodType.In(0) != reflect.TypeOf((*context.Context)(nil)).Elem() ||
			methodType.In(1).Kind() != reflect.Pointer {
			continue
		}

		// Ensure the method name matches the expected naming convention
		requestTypeName := methodType.In(1).Elem().Name()
		if method.Name != ("Validate" + requestTypeName) {
			continue
		}

		// Ensure the return type is error
		if methodType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		klog.V(4).InfoS("Registering validator", "validator", requestTypeName)
		funcs[requestTypeName] = methodValue
	}

	return funcs
}

// ValidRequired 验证结构体中的必需字段是否存在且不为空.
func ValidRequired(obj any, requiredFields ...string) error {
	val := reflect.ValueOf(obj)

	// 检查 obj 是否为结构体或指向结构体的指针
	if val.Kind() != reflect.Struct && val.Kind() != reflect.Ptr {
		return fmt.Errorf("input must be a struct or a pointer to a struct. Got %s", val.Kind().String())
	}

	// 如果是指针，获取指向的结构体值
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	// 仍需确保最终值是结构体
	if val.Kind() != reflect.Struct {
		return fmt.Errorf("input must be a struct or a pointer to a struct. Got %s", val.Kind().String())
	}

	// 遍历需要验证的字段
	for _, field := range requiredFields {
		// 使用反射获取字段
		fieldVal := val.FieldByName(field)

		// 判断字段是否存在
		if !fieldVal.IsValid() {
			return fmt.Errorf("field %s does not exist in struct", field)
		}

		// 检查字段是否为 nil
		if fieldVal.IsNil() {
			return fmt.Errorf("field %s must be provided", field)
		}
	}

	return nil
}
