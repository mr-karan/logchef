import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { thresholdColor, ruleMatches, tableRuleColor, formatPanelTime, formattedTableCell, validatePresentation } from "../panelPresentation";
import { parseExact } from "../exactNumber";
import { resultFrame } from "../panelFrame";
import type { TableRule, ThresholdStep } from "@/api/dashboards";
const steps: ThresholdStep[] = [{ value: null, color: "green" }, { value: "9007199254740993.01", color: "red" }];
it("thresholdColor preserves exact steps above 2^53 and decimal boundaries", () => {
 expect(thresholdColor(parseExact("9007199254740993.009"), steps)).toBe("green");
 expect(thresholdColor(parseExact("9007199254740993.01"), steps)).toBe("red");
 expect(thresholdColor(null, steps)).toBeUndefined();
 expect(thresholdColor(100,[{value:null,color:"blue"},{value:100,color:"amber"}])).toBe("amber");
});
const rule: TableRule = { columns:["details"], op:"contains", value:"retry", color:"red", scope:"row" };
it("rules highlight both scopes and match hidden columns", () => {
 const row = { details:"please retry", other:"ok" };
 expect(tableRuleColor(row,[rule])).toBe("red");
 expect(tableRuleColor(row,[rule],"details")).toBeUndefined();
 expect(tableRuleColor(row,[{ ...rule,scope:"cell" }],"details")).toBe("red");
 expect(tableRuleColor(row,[{ ...rule,scope:"cell" }],"other")).toBeUndefined();
 expect(tableRuleColor({details:"ok"},[rule])).toBeUndefined();
});
it("numeric rules compare exactly and all predicates handle nulls", () => {
 expect(ruleMatches(parseExact("9007199254740993"),{...rule,op:">",value:"9007199254740992"})).toBe(true);
 for (const [op,value,expected] of [["=","retry",true],["!=","x",true],["not_contains","x",true],["empty",undefined,false],["not_empty",undefined,true]] as const) expect(ruleMatches("retry",{...rule,op,value})).toBe(expected);
 expect(ruleMatches(null,{...rule,op:"empty",value:undefined})).toBe(true);
 expect(ruleMatches(null,{...rule,op:"not_contains"})).toBe(false);
});
it("time formats show seconds, milliseconds, timezone, and stable calendar dates", () => {
 expect(formatPanelTime("2026-11-12T23:05:09.123Z","time",{kind:"time"},"Asia/Kolkata")).toBe("2026-11-13 04:35:09");
 expect(formatPanelTime("2026-11-12T23:05:09.123Z","time",{kind:"time",date:"none",time:"millis"},"UTC")).toBe("23:05:09.123");
 expect(formatPanelTime("2026-11-12","date",{kind:"time"},"America/Los_Angeles")).toBe("2026-11-12");
 expect(formatPanelTime("2026-11-12T23:05:09Z","time",{kind:"time",date:"medium",time:"minutes"},"UTC","en-GB")).toBe("12 Nov 2026 23:05");
 const frame = resultFrame({columns:[{name:"n",type:"UInt64"}],rows:[{n:"9007199254740993"}]});
 expect(formattedTableCell(frame.rows[0].n,frame.columns[0],{decimals:2},"UTC","en-US")).toBe("9,007,199,254,740,993.00");
});
it("validates exact bounds, ordered steps, palette, rule and format variants", () => {
 expect(validatePresentation({type:"stat",thresholds:steps})).toBeNull();
 expect(validatePresentation({type:"stat",thresholds:[...steps,{value:"9007199254740993.009",color:"amber"}]})).not.toBeNull();
 expect(validatePresentation({type:"stat",thresholds:[{value:null,color:"red"},{value:"1e401",color:"blue"}]})).not.toBeNull();
 expect(validatePresentation({type:"table",rules:[rule],column_formats:{ts:{kind:"time",time:"seconds"}}})).toBeNull();
 expect(validatePresentation({type:"table",rules:[{...rule,op:">",value:"NaN"}]})).not.toBeNull();
});
describe("palette contrast", () => {
 const css = readFileSync("src/assets/index.css","utf8");
 const luminance = (hex:string) => {
  const channels = hex.match(/\w\w/g)!.map(c=>parseInt(c,16)/255).map(c=>c<=0.04045 ? c/12.92 : ((c+0.055)/1.055)**2.4);
  return channels[0]*0.2126+channels[1]*0.7152+channels[2]*0.0722;
 };
 for (const theme of [css.split('.dark {')[0],css.split('.dark {')[1]]) it("keeps every text/background pair above 4.5",()=>{
  for (const color of ["green","amber","red","blue","purple","gray"]) {
   const fg = theme.match(new RegExp(`--panel-${color}: #(\\w{6})`))![1];
   const bg = theme.match(new RegExp(`--panel-${color}-background: #(\\w{6})`))![1];
   const values=[luminance(fg),luminance(bg)].sort((a,b)=>a-b);
   expect((values[1]+0.05)/(values[0]+0.05)).toBeGreaterThanOrEqual(4.5);
  }
 });
});
