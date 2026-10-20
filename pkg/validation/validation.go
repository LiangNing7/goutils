package validation

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/klog/v2"
)

// Validator implements the validate.IValidator interface.
// Validator 在构造完成后只读，可安全地被多个 goroutine 并发使用.
type Validator struct {
	registry map[string]reflect.Value
}

// ProviderSet is the validator providers.
// var ProviderSet = wire.NewSet(NewValidator)

// NewValidator creates and initializes a custom validator.
func NewValidator(customValidator any) *Validator {
	if customValidator == nil {
		// 允许传入 nil，返回一个不包含任何校验器的实例.
		return &Validator{registry: map[string]reflect.Value{}}
	}
	return &Validator{registry: extractValidationMethods(customValidator)}
}

// Validate validates the request using the appropriate validation method.
// 未注册对应类型、request 为 nil 或不是指向结构体的指针时，直接返回 nil（不做校验）.
func (v *Validator) Validate(ctx context.Context, request any) error {
	if request == nil || v == nil {
		return nil
	}

	typ := reflect.TypeOf(request)
	// 只处理指向结构体的指针，其余情况没有可用的校验器.
	if typ == nil || typ.Kind() != reflect.Pointer || typ.Elem().Kind() != reflect.Struct {
		return nil
	}

	validationFunc, ok := v.registry[typeKey(typ.Elem())]
	if !ok {
		return nil // No validation function found for the request type
	}

	result := validationFunc.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(request)})
	if !result[0].IsNil() {
		return result[0].Interface().(error)
	}

	return nil
}

// typeKey 返回类型的唯一标识（包路径 + 类型名）.
// 直接使用 Name() 会让不同包中的同名类型（例如 a.User 与 b.User）串用同一个校验器.
func typeKey(t reflect.Type) string {
	if t.PkgPath() == "" {
		return t.String()
	}
	return t.PkgPath() + "." + t.Name()
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

		// 使用完整类型路径作为 key，避免不同包中的同名类型串用同一个校验器.
		key := typeKey(methodType.In(1).Elem())
		klog.V(4).InfoS("Registering validator", "validator", key)
		funcs[key] = methodValue
	}

	return funcs
}

// ValidRequired 验证结构体中的必需字段是否存在且不为空.
//
// 判定规则：字段的零值视为“未提供”。
// 对于指针、slice、map、interface 等可空类型即 nil；对于 string/int 等不可空类型即空字符串 / 0 值。
// 入参为 nil、nil 指针或非结构体时返回错误（不会 panic）。
func ValidRequired(obj any, requiredFields ...string) error {
	if obj == nil {
		return fmt.Errorf("input must be a struct or a pointer to a struct. Got nil")
	}

	val := reflect.ValueOf(obj)

	// 检查 obj 是否为结构体或指向结构体的指针
	if val.Kind() != reflect.Struct && val.Kind() != reflect.Ptr {
		return fmt.Errorf("input must be a struct or a pointer to a struct. Got %s", val.Kind().String())
	}

	// 如果是指针，获取指向的结构体值
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return fmt.Errorf("input must be a struct or a pointer to a struct. Got %s", val.Kind().String())
		}
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

		// 检查字段是否为空（nil 或零值），此处不能用 IsNil：
		// string/int 等不可空类型调用 IsNil 会 panic（reflect: call of reflect.Value.IsNil on string Value）.
		if fieldVal.IsZero() {
			return fmt.Errorf("field %s must be provided", field)
		}
	}

	return nil
}
