import { describe, it, expect, beforeEach } from "vitest"
import { ref, nextTick } from "vue"
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

  describe("persistKey", () => {
    const opts = {
      searchKeys: ["name" as const],
      sortAccessors: { name: (r: Row) => r.name.toLowerCase(), updated: (r: Row) => r.updated },
      initialSort: { key: "name", dir: "asc" as const },
      persistKey: "test",
    }

    beforeEach(() => localStorage.clear())

    it("restores the last chosen sort on the next mount", async () => {
      const first = useTableSearchSort(ref(data), opts)
      first.toggleSort("updated")
      first.toggleSort("updated")
      await nextTick()

      const second = useTableSearchSort(ref(data), opts)
      expect(second.sortKey.value).toBe("updated")
      expect(second.sortDir.value).toBe("desc")
      expect(names(second.rows.value)).toEqual(["Alpha", "gamma", "beta"])
    })

    it("falls back to the initial sort when the stored value is stale or corrupt", () => {
      localStorage.setItem("logchef_list_sort_test", JSON.stringify({ key: "removed", dir: "asc" }))
      expect(useTableSearchSort(ref(data), opts).sortKey.value).toBe("name")

      localStorage.setItem("logchef_list_sort_test", JSON.stringify({ key: "__proto__", dir: "asc" }))
      const proto = useTableSearchSort(ref(data), opts)
      expect(proto.sortKey.value).toBe("name")
      expect(names(proto.rows.value)).toEqual(["Alpha", "beta", "gamma"])

      localStorage.setItem("logchef_list_sort_test", "{not json")
      expect(useTableSearchSort(ref(data), opts).sortKey.value).toBe("name")
    })
  })
})
