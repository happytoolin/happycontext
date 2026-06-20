package hc

func writeEventToSink(sink Sink, event *Event, level Level, message string, mapper FieldMapper) {
	fields := applyFieldMapper(event.fieldsSnapshot(), mapper)
	sink.Write(level, message, fields)
}

func writeEventToSinkWithCompletion(sink Sink, event *Event, level Level, message string, durationMS int64, code int, outcome Outcome, mapper FieldMapper) {
	fields := event.fieldsSnapshotWithExtra(3)
	if fields == nil {
		fields = make(map[string]any, 3)
	}
	fields["duration_ms"] = durationMS
	fields["op.code"] = code
	fields["op.outcome"] = string(outcome)
	fields = applyFieldMapper(fields, mapper)
	sink.Write(level, message, fields)
}

func writeEventToSinkWithStartAndCompletion(sink Sink, event *Event, level Level, message string, start OperationStart, durationMS int64, code int, outcome Outcome, mapper FieldMapper) {
	fields := event.fieldsSnapshotWithExtra(operationFieldCount(start) + 3)
	if fields == nil {
		fields = make(map[string]any, operationFieldCount(start)+3)
	}
	addOperationFieldsToMap(fields, start)
	fields["duration_ms"] = durationMS
	fields["op.code"] = code
	fields["op.outcome"] = string(outcome)
	fields = applyFieldMapper(fields, mapper)
	sink.Write(level, message, fields)
}

func writeEventToSinkWithStartAndCompletionUnsafe(sink Sink, event *Event, level Level, message string, start OperationStart, durationMS int64, code int, outcome Outcome, mapper FieldMapper) {
	writeEventToSinkWithStartAndCompletion(sink, event, level, message, start, durationMS, code, outcome, mapper)
}

func operationFieldCount(start OperationStart) int {
	count := 2
	if start.ID != "" {
		count++
	}
	if start.Source != "" {
		count++
	}
	if start.Attempt > 0 {
		count++
	}
	if start.MaxAttempts > 0 {
		count++
	}
	return count
}

func addOperationFieldsToMap(fields map[string]any, start OperationStart) {
	fields["op.domain"] = string(start.Domain)
	fields["op.name"] = start.Name
	if start.ID != "" {
		fields["op.id"] = start.ID
	}
	if start.Source != "" {
		fields["op.source"] = start.Source
	}
	if start.Attempt > 0 {
		fields["op.attempt"] = start.Attempt
	}
	if start.MaxAttempts > 0 {
		fields["op.max_attempts"] = start.MaxAttempts
	}
}
