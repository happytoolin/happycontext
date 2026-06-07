package hc

func writeEventToSink(sink Sink, event *Event, level Level, message string, mapper FieldMapper) {
	if mapper == nil {
		if borrowedSink, ok := sink.(borrowedFieldSink); ok {
			if event.withBorrowedFieldList(func(fields []BorrowedField) {
				borrowedSink.WriteBorrowedFields(level, message, fields)
			}) {
				return
			}
		}

		if fieldSink, ok := sink.(fieldSink); ok {
			if event.withFieldList(func(fields []Field) {
				fieldSink.WriteFields(level, message, fields)
			}) {
				return
			}
		}

		if unsafeSink, ok := sink.(unsafeMapSink); ok {
			if event.withFieldMap(func(fields map[string]any) {
				unsafeSink.WriteUnsafe(level, message, fields)
			}) {
				return
			}
		}
	}

	fields := applyFieldMapper(event.fieldsSnapshot(), mapper)
	sink.Write(level, message, fields)
}

func writeEventToSinkWithCompletion(sink Sink, event *Event, level Level, message string, durationMS int64, code int, outcome Outcome, mapper FieldMapper) {
	if mapper == nil {
		if completionSink, ok := sink.(borrowedCompletionFieldSink); ok {
			if event.withBorrowedFieldList(func(fields []BorrowedField) {
				completionSink.WriteBorrowedFieldsWithCompletion(level, message, fields, durationMS, code, outcome)
			}) {
				return
			}
		}

		if completionSink, ok := sink.(completionFieldSink); ok {
			if event.withFieldList(func(fields []Field) {
				completionSink.WriteFieldsWithCompletion(level, message, fields, durationMS, code, outcome)
			}) {
				return
			}
		}
	}

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
	if mapper == nil {
		if operationSink, ok := sink.(borrowedOperationCompletionFieldSink); ok {
			if event.withBorrowedFieldList(func(fields []BorrowedField) {
				operationSink.WriteBorrowedFieldsWithOperationCompletion(level, message, fields, start, durationMS, code, outcome)
			}) {
				return
			}
		}

		if operationSink, ok := sink.(operationCompletionFieldSink); ok {
			if event.withFieldList(func(fields []Field) {
				operationSink.WriteFieldsWithOperationCompletion(level, message, fields, start, durationMS, code, outcome)
			}) {
				return
			}
		}
	}

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
	if mapper == nil {
		if operationSink, ok := sink.(borrowedOperationCompletionFieldSink); ok {
			if event.withBorrowedFieldListUnsafe(func(fields []BorrowedField) {
				operationSink.WriteBorrowedFieldsWithOperationCompletion(level, message, fields, start, durationMS, code, outcome)
			}) {
				return
			}
		}

		if operationSink, ok := sink.(operationCompletionFieldSink); ok {
			if event.withFieldListUnsafe(func(fields []Field) {
				operationSink.WriteFieldsWithOperationCompletion(level, message, fields, start, durationMS, code, outcome)
			}) {
				return
			}
		}
	}

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
