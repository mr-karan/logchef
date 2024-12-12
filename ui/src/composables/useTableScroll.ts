import { ref, type Ref } from 'vue'

export function useTableScroll() {
    const tableWrapperRef = ref<HTMLElement | null>(null)

    const scrollToTop = () => {
        if (tableWrapperRef.value) {
            tableWrapperRef.value.scrollTo({
                top: 0,
                behavior: 'smooth'
            })
        }
    }

    // Updated to handle Vue component refs
    const setTableWrapper = (el: any) => {
        // Get the actual DOM element from the ref
        const domElement = el?.$el || el
        if (domElement instanceof HTMLElement) {
            tableWrapperRef.value = domElement.querySelector('.p-datatable-wrapper')
        }
    }

    return {
        tableWrapperRef,
        scrollToTop,
        setTableWrapper
    }
}