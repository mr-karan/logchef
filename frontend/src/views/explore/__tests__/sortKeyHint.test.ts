import { describe, expect, it } from "vitest";
import { unfilteredSortKeys } from "../sortKeyHint";

const keys = ["namespace", "job_name", "_timestamp"];

describe("unfilteredSortKeys", () => {
  it("suggests the non-timestamp sort keys when the query filters on none of them", () => {
    expect(unfilteredSortKeys('level="error"', "logchefql", keys, "_timestamp")).toEqual(["namespace", "job_name"]);
    expect(unfilteredSortKeys("", "logchefql", keys, "_timestamp")).toEqual(["namespace", "job_name"]);
  });

  it("suggests nothing once a LogchefQL condition uses any sort key", () => {
    expect(unfilteredSortKeys('namespace="aux" and level="error"', "logchefql", keys, "_timestamp")).toEqual([]);
    expect(unfilteredSortKeys('(job_name != "x")', "logchefql", keys, "_timestamp")).toEqual([]);
  });

  it("does not mistake a longer field or a value for the sort key", () => {
    expect(unfilteredSortKeys('my_namespace="aux"', "logchefql", keys, "_timestamp")).toEqual(["namespace", "job_name"]);
    expect(unfilteredSortKeys('msg="namespace=aux"', "logchefql", keys, "_timestamp")).toEqual(["namespace", "job_name"]);
  });

  it("reads native SQL filters only after WHERE", () => {
    expect(unfilteredSortKeys("SELECT namespace FROM logs", "native", keys, "_timestamp")).toEqual(["namespace", "job_name"]);
    expect(unfilteredSortKeys("SELECT * FROM logs WHERE `namespace` = 'aux'", "native", keys, "_timestamp")).toEqual([]);
  });

  it("skips expression sort keys", () => {
    expect(unfilteredSortKeys("", "logchefql", ["toYYYYMMDD(_timestamp)", "service"], "_timestamp")).toEqual(["service"]);
  });
});
