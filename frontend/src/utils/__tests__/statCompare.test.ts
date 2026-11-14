import { describe, expect, it } from "vitest";
import { reduceStat, resultFrame } from "@/utils/panelFrame";
import { exactString, isExact } from "@/utils/exactNumber";

const row = (day: string | null, n: string | null) => ({ day, n });
const frame = (rows: ReturnType<typeof row>[]) => resultFrame({ columns: [{ name: "day", type: "Date" }, { name: "n", type: "Decimal(30, 2)" }], rows });
const options = { value: "n", orderBy: "day", reduce: "last", count: false, compare: { mode: "previous_row", good: "up" }, sparkline: true } as const;
const text = (n: unknown) => isExact(n) ? exactString(n) : n;
describe("stat comparison", () => {
 it("one row has no comparison", () => { expect(reduceStat(frame([row("2026-11-12", "5")]), options).comparison).toBeUndefined(); });
 it.each([[null, "5"], ["5", null]])("NULL on either side %s %s", (a, b) => {
  const stat = reduceStat(frame([row("2026-11-11", a), row("2026-11-12", b)]), options);
  expect(stat.comparison).toBeUndefined(); expect(text(stat.value)).toBe(b); expect(stat.rowLabel).toBe("2026-11-12");
 });
 it("previous zero keeps absolute change without percent", () => {
  const stat = reduceStat(frame([row("2026-11-11", "0"), row("2026-11-12", "5")]), options);
  expect(text(stat.comparison?.delta)).toBe("5"); expect(stat.comparison?.percent).toBeNull();
 });
 it("missing day compares actual neighbor dates", () => {
  const stat = reduceStat(frame([row("2026-11-09", "10"), row("2026-11-12", "15")]), options);
  expect(stat.comparison?.rowLabel).toBe("2026-11-09"); expect(stat.comparison?.percent).toBe(50); expect(stat.points).toHaveLength(2);
 });
 it("tie is stable and reports every tied row", () => {
  const stat = reduceStat(frame([row("2026-11-12", "10"), row("2026-11-12", "15")]), options);
  expect(text(stat.value)).toBe("15"); expect(text(stat.comparison?.delta)).toBe("5"); expect(stat.notices.join()).toContain("2 rows share");
 });
 it("unordered input sorts four days for value, neighbor and sparkline", () => {
  const stat = reduceStat(frame([row("2026-11-12", "40"), row("2026-11-09", "10"), row("2026-11-11", "30"), row("2026-11-10", "20")]), options);
  expect(text(stat.value)).toBe("40"); expect(stat.rowLabel).toBe("2026-11-12"); expect(stat.comparison?.rowLabel).toBe("2026-11-11"); expect(text(stat.comparison?.delta)).toBe("10"); expect(stat.points?.map(p => text(p.value))).toEqual(["10", "20", "30", "40"]);
 });
 it("first compares its following row and none disables comparison", () => {
  const f = frame([row("2026-11-12", "40"), row("2026-11-09", "10")]);
  expect(text(reduceStat(f, { ...options, reduce: "first" }).comparison?.delta)).toBe("-30");
  expect(reduceStat(f, { ...options, compare: { mode: "none" } }).comparison).toBeUndefined();
 });
 it("subtracts large decimals exactly and divides without overflow", () => {
  const stat = reduceStat(frame([row("2026-11-11", "9007199254740993.01"), row("2026-11-12", "9007199254740994.02")]), options);
  expect(text(stat.comparison?.delta)).toBe("1.01"); expect(stat.comparison?.percent).toBeGreaterThan(0);
 });
 it("invalid order rows do not become neighbors or sparkline points", () => {
  const stat = reduceStat(frame([row("2026-11-11", "10"), row(null, "90"), row("2026-11-12", "20")]), options);
  expect(text(stat.comparison?.delta)).toBe("10"); expect(stat.points).toHaveLength(2); expect(stat.notices.join()).toContain("1 row(s)");
 });
 it("aggregate reducers keep all values while sparkline uses ordered rows", () => {
  const stat = reduceStat(frame([row("2026-11-11", "10"), row(null, "90"), row("2026-11-12", "20")]), { ...options, reduce: "sum", compare: { mode: "none" } });
  expect(text(stat.value)).toBe("120"); expect(stat.points).toHaveLength(2);
 });
});

import trends from "./fixtures/statTrends.json";
it.each(trends)("corpus card $card uses its date role independently of SQL ordering", fixture => {
 const rows = fixture.card === 584 ? [row("2026-11-12", "40")] : [row("2026-11-12", "40"), row("2026-11-09", "10"), row("2026-11-11", "30"), row("2026-11-10", "20")];
 const stat = reduceStat(frame(rows), { ...options, compare: { mode: fixture.compare === "none" ? "none" : "previous_row" } });
 expect(text(stat.value)).toBe("40");
 if (fixture.card === 584) expect(stat.comparison).toBeUndefined();
 else { expect(text(stat.comparison?.delta)).toBe("10"); expect(stat.comparison?.rowLabel).toBe("2026-11-11"); }
 expect(/ORDER BY/i.test(fixture.sql)).toBe(fixture.card === 271);
});

import { validatePanelsBlob } from "@/utils/dashboardPanels";
import type { DashboardViz } from "@/api/dashboards";
it("validates stat comparison ordering and reducer constraints",()=>{
 const validate=(viz:DashboardViz)=>validatePanelsBlob({version:2,features:[],tabs:[{id:"main",title:"Main",layout:[{id:"p",x:0,y:0,w:6,h:2}]}],panels:[{id:"p",kind:"query",title:"Trend",team_id:1,source_id:1,query:{mode:"result",language:"clickhouse-sql",text:"SELECT day, n FROM events"},viz}]});
 expect(validate({type:"stat",value:"n",order_by:"day",compare:{mode:"previous_row"},sparkline:true})).toBeNull();
 expect(validate({type:"stat",value:"n",compare:{mode:"previous_row"}})).toContain('order_by');
 expect(validate({type:"stat",value:"n",order_by:"day",reduce:"sum",compare:{mode:"previous_row"}})).toContain('first or last');
 expect(validate({type:"stat",value:"n",sparkline:true})).toContain('order_by');
});
