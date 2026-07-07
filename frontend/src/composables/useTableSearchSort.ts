import { ref, computed, unref, type Ref } from "vue"

export type SortDir = "asc" | "desc"

type SearchKey<T> = keyof T | ((row: T) => string | number | null | undefined)
type SortValue = string | number | Date

/**
 * useTableSearchSort — client-side search + single-column sort for a fully
 * loaded list. The standard pattern behind our member/user/token/source tables:
 * a search box filters across `searchKeys`, and clickable headers toggle sort
 * via named `sortAccessors`.
 *
 *   const { search, rows, sortKey, sortDir, toggleSort } = useTableSearchSort(members, {
 *     searchKeys: ['email', (m) => m.full_name],
 *     sortAccessors: { role: (m) => m.role, added: (m) => new Date(m.created_at) },
 *     initialSort: { key: 'added', dir: 'desc' },
 *   })
 */
export function useTableSearchSort<T>(
  source: Ref<T[]> | (() => T[]),
  opts: {
    searchKeys: SearchKey<T>[]
    sortAccessors?: Record<string, (row: T) => SortValue>
    initialSort?: { key: string; dir: SortDir }
  },
) {
  const getRows = typeof source === "function" ? source : () => unref(source)
  const search = ref("")
  const sortKey = ref(opts.initialSort?.key ?? "")
  const sortDir = ref<SortDir>(opts.initialSort?.dir ?? "asc")

  function fieldStr(row: T, key: SearchKey<T>): string {
    const v = typeof key === "function" ? key(row) : (row[key] as unknown)
    return v == null ? "" : String(v).toLowerCase()
  }

  const rows = computed<T[]>(() => {
    let out = (getRows() ?? []).slice()

    const q = search.value.trim().toLowerCase()
    if (q) {
      out = out.filter((row) => opts.searchKeys.some((k) => fieldStr(row, k).includes(q)))
    }

    const accessor = sortKey.value ? opts.sortAccessors?.[sortKey.value] : undefined
    if (accessor) {
      const dir = sortDir.value === "asc" ? 1 : -1
      out.sort((a, b) => {
        const av = accessor(a)
        const bv = accessor(b)
        let cmp: number
        if (av instanceof Date && bv instanceof Date) cmp = av.getTime() - bv.getTime()
        else if (typeof av === "number" && typeof bv === "number") cmp = av - bv
        else cmp = String(av).localeCompare(String(bv))
        return cmp * dir
      })
    }
    return out
  })

  function toggleSort(key: string) {
    if (sortKey.value === key) {
      sortDir.value = sortDir.value === "asc" ? "desc" : "asc"
    } else {
      sortKey.value = key
      sortDir.value = "asc"
    }
  }

  return { search, sortKey, sortDir, rows, toggleSort }
}
