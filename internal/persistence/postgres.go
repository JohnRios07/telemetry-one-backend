package persistence

const DriverPostgres = "postgres"

type StorageDecision struct {
	Driver string
	Reason string
}

func DefaultStorageDecision() StorageDecision {
	return StorageDecision{
		Driver: DriverPostgres,
		Reason: "reference spec requires PostgreSQL; Phase 1.4 keeps runtime wiring out and defines schema plus repository boundaries only",
	}
}
