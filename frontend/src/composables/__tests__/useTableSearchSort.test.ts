import { describe, it, expect } from "vitest"
import { ref } from "vue"
import { useTableSearchSort } from "../useTableSearchSort"

type Row = { name: string; updated: number; triggered: string | null }

const data: Row[] = [
  { name: "beta", updated: 1, triggered: null },
  { name: "Alpha", updated: 3, triggered: "2026-01-02T00:00:00Z" },
  { name: "gamma", updated: 2, triggered: "2026-01-01T00:00:00Z" },
]

const names = (rows: Row[]) => rows.map((r) => r.name)

describe("useTableSearchSort", () => {
  it("keeps source order until a column is clicked, then cycles asc → desc → source order", () => {
    const s = useTableSearchSort(ref(data), {
      searchKeys: ["name"],
      sortAccessors: { name: (r) => r.name.toLowerCase() },
    })
    expect(names(s.rows.value)).toEqual(["beta", "Alpha", "gamma"])
    s.toggleSort("name")
    expect(names(s.rows.value)).toEqual(["Alpha", "beta", "gamma"])
    s.toggleSort("name")
    expect(names(s.rows.value)).toEqual(["gamma", "beta", "Alpha"])
    s.toggleSort("name")
    expect(s.sortKey.value).toBe("")
    expect(names(s.rows.value)).toEqual(["beta", "Alpha", "gamma"])
  })

  it("returns to the initial sort after another column's desc", () => {
    const s = useTableSearchSort(ref(data), {
      searchKeys: ["name"],
      sortAccessors: { name: (r) => r.name.toLowerCase(), updated: (r) => r.updated },
      initialSort: { key: "updated", dir: "desc" },
    })
    expect(names(s.rows.value)).toEqual(["Alpha", "gamma", "beta"])
    s.toggleSort("name")
    s.toggleSort("name")
    s.toggleSort("name")
    expect(s.sortKey.value).toBe("updated")
    expect(s.sortDir.value).toBe("desc")
  })

  it("flips direction instead of sticking when the initial column is desc", () => {
    const s = useTableSearchSort(ref(data), {
      searchKeys: ["name"],
      sortAccessors: { updated: (r) => r.updated },
      initialSort: { key: "updated", dir: "desc" },
    })
    s.toggleSort("updated")
    expect(s.sortDir.value).toBe("asc")
    expect(names(s.rows.value)).toEqual(["beta", "gamma", "Alpha"])
    s.toggleSort("updated")
    expect(s.sortDir.value).toBe("desc")
  })

  it("sorts dates and filters by search together", () => {
    const s = useTableSearchSort(ref(data), {
      searchKeys: ["name"],
      sortAccessors: { triggered: (r) => new Date(r.triggered ?? 0) },
    })
    s.toggleSort("triggered")
    expect(names(s.rows.value)).toEqual(["beta", "gamma", "Alpha"])
    s.search.value = "a"
    expect(names(s.rows.value)).toEqual(["beta", "gamma", "Alpha"])
    s.search.value = "alp"
    expect(names(s.rows.value)).toEqual(["Alpha"])
  })
})
