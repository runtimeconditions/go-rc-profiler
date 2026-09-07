// Package commonintegrations provides typed no-op declaration helpers for the
// Common Integrations Runtime Conditions extension.
package commonintegrations

// Declaration is the inert value returned by top-level condition declarations.
type Declaration struct{}

// Engine identifies a concrete integration engine within an interface family.
type Engine string

const (
	Postgres  Engine = "postgres"
	MySQL     Engine = "mysql"
	MariaDB   Engine = "mariadb"
	SQLServer Engine = "sqlserver"
	Oracle    Engine = "oracle"
	SQLite    Engine = "sqlite"

	MongoDB   Engine = "mongodb"
	Couchbase Engine = "couchbase"

	Redis     Engine = "redis"
	Memcached Engine = "memcached"
)

// APIOption configures an API declaration.
type APIOption interface {
	CommonIntegrationsAPIOption()
}

// OperationOption configures an API operation declaration.
type OperationOption interface {
	CommonIntegrationsOperationOption()
}

// SchemaOption configures an API declaration or operation with a schema.
type SchemaOption interface {
	APIOption
	OperationOption
}

type apiOption struct{}

func (apiOption) CommonIntegrationsAPIOption() {}

type operationOption struct{}

func (operationOption) CommonIntegrationsOperationOption() {}

type schemaOption struct{}

func (schemaOption) CommonIntegrationsAPIOption()       {}
func (schemaOption) CommonIntegrationsOperationOption() {}

// API declares an external API dependency.
func API(name string, options ...APIOption) Declaration {
	return Declaration{}
}

// Spec declares an external API specification reference.
func Spec(format, uri string, version ...string) APIOption {
	return apiOption{}
}

// GET declares an HTTP GET operation dependency.
func GET(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// HEAD declares an HTTP HEAD operation dependency.
func HEAD(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// POST declares an HTTP POST operation dependency.
func POST(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// PUT declares an HTTP PUT operation dependency.
func PUT(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// PATCH declares an HTTP PATCH operation dependency.
func PATCH(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// DELETE declares an HTTP DELETE operation dependency.
func DELETE(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// OPTIONS declares an HTTP OPTIONS operation dependency.
func OPTIONS(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// TRACE declares an HTTP TRACE operation dependency.
func TRACE(path string, options ...OperationOption) APIOption {
	return apiOption{}
}

// Request attaches a request body schema based on T.
func Request[T any]() SchemaOption {
	return schemaOption{}
}

// Response attaches a response body schema based on T.
func Response[T any]() SchemaOption {
	return schemaOption{}
}

// DatastoreOption configures a datastore declaration.
type DatastoreOption interface {
	CommonIntegrationsDatastoreOption()
}

type datastoreOption struct{}

func (datastoreOption) CommonIntegrationsDatastoreOption() {}

// Datastore declares a persistent datastore dependency.
func Datastore(name string, options ...DatastoreOption) Declaration {
	return Declaration{}
}

// Relational declares a relational datastore interface and engine.
func Relational(engine Engine) DatastoreOption {
	return datastoreOption{}
}

// Document declares a document datastore interface and engine.
func Document(engine Engine) DatastoreOption {
	return datastoreOption{}
}

// CacheOption configures a cache declaration.
type CacheOption interface {
	CommonIntegrationsCacheOption()
}

type cacheOption struct{}

func (cacheOption) CommonIntegrationsCacheOption() {}

// Cache declares a volatile cache dependency.
func Cache(name string, options ...CacheOption) Declaration {
	return Declaration{}
}

// KeyValue declares a key/value cache interface and engine.
func KeyValue(engine Engine) CacheOption {
	return cacheOption{}
}
