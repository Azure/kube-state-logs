package testutils

type MockLogger struct {
	logs []any
}

func (m *MockLogger) Log(entry any) error {
	m.logs = append(m.logs, entry)
	return nil
}

func (m *MockLogger) GetLogs() []any {
	return m.logs
}

func (m *MockLogger) Clear() {
	m.logs = nil
}
