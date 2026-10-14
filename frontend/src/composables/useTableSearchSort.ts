import { ref, computed, unref, watch, type Ref } from "vue"

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
 *     persistKey: 'team_members',
 *   })
 *
 * With `persistKey`, the chosen sort is saved in localStorage under
 * `logchef_list_sort_<persistKey>` and restored on the next visit. A stored key
 * that no longer names a sort accessor is ignored.
 */
export function useTableSearchSort<T>(
  source: Ref<T[]> | (() => T[]),
  opts: {
    searchKeys: SearchKey<T>[]
    sortAccessors?: Record<string, (row: T) => SortValue>
    initialSort?: { key: string; dir: SortDir }
    persistKey?: string
  },
) {
  const getRows = typeof source === "function" ? source : () => unref(source)
  const storageKey = opts.persistKey ? `logchef_list_sort_${opts.persistKey}` : null
  const restored = storageKey ? readStoredSort(storageKey, opts.sortAccessors ?? {}) : null
  const search = ref("")
  const sortKey = ref(restored?.key ?? opts.initialSort?.key ?? "")
  const sortDir = ref<SortDir>(restored?.dir ?? opts.initialSort?.dir ?? "asc")

  if (storageKey) {
    watch([sortKey, sortDir], ([key, dir]) => {
      try {
        localStorage.setItem(storageKey, JSON.stringify({ key, dir }))
      } catch {
        // Storage can be full or disabled; the sort still works for this visit.
      }
    })
  }

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

  // Clicking a column cycles asc → desc → back to the initial sort, so a list
  // with a meaningful natural order (e.g. pinned items) can be restored. When
  // the column already is the initial sort, it just flips direction.
  function toggleSort(key: string) {
    const init = opts.initialSort
    if (sortKey.value === key && sortDir.value === "asc") {
      sortDir.value = "desc"
    } else if (sortKey.value === key && !(init?.key === key && init.dir === "desc")) {
      sortKey.value = init?.key ?? ""
      sortDir.value = init?.dir ?? "asc"
    } else {
      sortKey.value = key
      sortDir.value = "asc"
    }
  }

  return { search, sortKey, sortDir, rows, toggleSort }
}

function readStoredSort(
  storageKey: string,
  accessors: Record<string, unknown>,
): { key: string; dir: SortDir } | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(localStorage.getItem(storageKey) ?? "null")
  } catch {
    return null
  }
  if (typeof parsed !== "object" || parsed === null) return null
  const key: unknown = Reflect.get(parsed, "key")
  const dir: unknown = Reflect.get(parsed, "dir")
  if (typeof key !== "string" || (dir !== "asc" && dir !== "desc")) return null
  // An empty key is the "natural order" state of lists without an initial sort.
  if (key !== "" && !(Object.hasOwn(accessors, key) && typeof accessors[key] === "function")) return null
  return { key, dir }
}
