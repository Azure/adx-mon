package storage

// IntakeInFlight reports the intake memory s's imports hold.
func (s *LocalStore) IntakeInFlight() int64 { return s.intake.InFlight() }
