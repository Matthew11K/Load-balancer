package errors

type ErrConfigFileNotFound struct {
	FileName string
}

func (e *ErrConfigFileNotFound) Error() string {
	return "конфигурационный файл не найден: " + e.FileName
}

type ErrConfigFileParseError struct {
	Message string
}

func (e *ErrConfigFileParseError) Error() string {
	return "ошибка при разборе конфигурационного файла: " + e.Message
}

type ErrNoAvailableBackends struct{}

func (e *ErrNoAvailableBackends) Error() string {
	return "нет доступных бэкенд-серверов"
}

type ErrInvalidAlgorithm struct {
	Algorithm string
}

func (e *ErrInvalidAlgorithm) Error() string {
	return "неверный алгоритм балансировки: " + e.Algorithm
}

type ErrClientNotFound struct {
	ClientID string
}

func (e *ErrClientNotFound) Error() string {
	return "клиент не найден: " + e.ClientID
}

type ErrRateLimitExceeded struct {
	ClientID string
}

func (e *ErrRateLimitExceeded) Error() string {
	return "превышено ограничение скорости запросов для клиента: " + e.ClientID
}

type ErrDatabaseClientNotFound struct {
	ClientID string
}

func (e *ErrDatabaseClientNotFound) Error() string {
	return "клиент не найден в базе данных: " + e.ClientID
}

type ErrDatabaseError struct {
	Message string
}

func (e *ErrDatabaseError) Error() string {
	return "ошибка базы данных: " + e.Message
}
