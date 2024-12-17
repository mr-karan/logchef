Yes continue refactoring but I want to explain a few things

- In this `logs.go` we have query connectors to directly execute the queries with Clickhouse
- In @logchefql we have parsers to translate our custom language to create valid SQLs

Is there a way to merge both these code because in principle they are doing same stuff. We can have a basic query executor defined in @db so that both the modes work transparently with minimal stuff being repeated. does this make sense? Ifnot please ask me questions I'll be happy to provide more context.

For now, when user loads dashboard we
- fetch logs in a given defailt timestamp without filters for a particular source
- fetch schema of the table and then also fetch last ~100ish logs to find a common pattern of the schema in all logs if these logs are semi structured to detect child fields in JSON of clickhouse column
- then the user can narrow down logs by sending either a full fledged clickhouse sql or logchefql for which we have built parsers