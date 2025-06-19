package owl

import (
	"fmt"
	"log"
	"reflect"
)

// Builder is a function that expects anything and retuns
// the type that builds. The type cant be func() interface{}
// cause some errors appears in runtime. So it's represented
// as an interface.
type Builder any

// Runner is any funtion that returns void. It is use
// as an easy way to ask to the injetor to provide dependencies
// to do something. For example, imagine that we have an interface called
// UserDao and we just want do something with it outside any builder or
// dependecy created using this injector. The UserDao is registered
// this way:
//
//	injector.Add(db.NewUserDao)
//
// Then, you can ask for the dependency to the injector this way:
//
//	var userDao db.UserDao
//	userDao = injector.GetByType(reflect.TypeOf(&userDao).Elem()).(db.UserDao)
//
// Its pretty cumbersome huh? You have to do that because you know it is an interface.
// Using Runner you can just do this:
//
//	injector.Run(func(userDao db.UserDao) {
//		[... do whatever you want with userdDao ...]
//	})
//
// The callback function will be exectued inmediatly.
type Runner any

// Injector is an automated dependency injector inspired in Sping's
// DI. It will detect which builder to call using its return type.
// If the builder haver params, it will fullfill that params calling
// other builders that provides its types.
type Injector struct {
	builders map[reflect.Type]Builder
}

// NewInjector with default values.
func NewInjector() *Injector {
	return &Injector{
		builders: make(map[reflect.Type]Builder),
	}
}

// Add a builder to the dependency injector.
func (injector *Injector) Add(builder Builder) {
	outputType := reflect.TypeOf(builder).Out(0)
	injector.builders[outputType] = builder
}

// ShowAvailableBuilders prints all registered builders.
func (injector Injector) ShowAvailableBuilders() {
	for k := range injector.builders {
		log.Printf("Builder for type: %s\n", k)
	}
}

// Get returns a builded dependency.
func (injector Injector) Get(name any) (any, error) {
	return injector.GetByType(reflect.TypeOf(name))
}

// GetByType returns a builded dependency identified by type.
func (injector Injector) GetByType(name reflect.Type) (any, error) {
	dependencyBuilder := injector.builders[name]
	if dependencyBuilder == nil {
		return nil, fmt.Errorf("builder not found for type %s", name)
	}
	return injector.CallBuilder(dependencyBuilder), nil
}

// ResolveHandler created by a builder.
func (injector Injector) ResolveHandler(builder Builder) Handler {
	return injector.CallBuilder(builder).(Handler)
}

// CallBuilder injecting all parameters with provided builders. If some parameter
// type cannot be found, it will panic.
func (injector Injector) CallBuilder(builder Builder) any {
	var inputs []reflect.Value
	builderType := reflect.TypeOf(builder)
	for i := range builderType.NumIn() {
		impl, err := injector.GetByType(builderType.In(i))
		if err != nil {
			panic(err)
		}
		inputs = append(inputs, reflect.ValueOf(impl))
	}
	builderVal := reflect.ValueOf(builder)
	builded := builderVal.Call(inputs)
	return builded[0].Interface()
}

// PopulateStruct fills a struct with the implementations
// that the injector can create. Make sure you pass a reference and
// not a value.
func (injector Injector) PopulateStruct(userStruct any) {
	ptrStructValue := reflect.ValueOf(userStruct)
	structValue := ptrStructValue.Elem()
	if structValue.Kind() != reflect.Struct {
		log.Panicln("Value passed to PopulateStruct is not a struct")
	}
	for i := range structValue.NumField() {
		field := structValue.Field(i)
		if field.IsValid() && field.CanSet() {
			impl, err := injector.GetByType(field.Type())
			if err != nil {
				panic(err)
			}
			field.Set(reflect.ValueOf(impl))
		}
	}
}

// Run is a function that runs a Runner. Show Runner type for more information.
func (injector Injector) Run(runner Runner) {
	var inputs []reflect.Value
	runnerType := reflect.TypeOf(runner)
	for i := range runnerType.NumIn() {
		impl, err := injector.GetByType(runnerType.In(i))
		if err != nil {
			panic(err)
		}
		inputs = append(inputs, reflect.ValueOf(impl))
	}
	runnerVal := reflect.ValueOf(runner)
	runnerVal.Call(inputs)
}

func (injector Injector) clone() *Injector {
	builders := make(map[reflect.Type]Builder)
	for k, v := range injector.builders {
		builders[k] = v
	}
	return &Injector{builders}
}
