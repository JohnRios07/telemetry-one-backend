package persistence

const DriverPostgres = "postgres"

type StorageDecision struct {
	Driver string
	Reason string
}

func DefaultStorageDecision() StorageDecision {
	return StorageDecision{
		Driver: DriverPostgres,
		Reason: "PostgreSQL is used when TELEMETRY_ONE_DATABASE_URL is configured; otherwise runtime falls back to in-memory repositories for local development and tests",
	}
}
