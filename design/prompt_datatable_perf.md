As an expert in high performance frontend apps, with specific expertise on rendering large data sets, I present you a challenge I am facing. I am using PrimeVue data table for rendering logs from Clickhouse. The app is similar to ELK, basically a log analytics application - It has to work with large volume of logs. I am facing issues with the Datatable and while I have done some amount of changes, they are clearly not enough and there must be scope to juice out max performance.

To begin with, I am adding the raw datatable.vue code from PrimeVue so that you understand it clearly

```
<template>
    <div :class="cx('root')" data-scrollselectors=".p-datatable-wrapper" v-bind="ptmi('root')">
        <slot></slot>
        <div v-if="loading" :class="cx('mask')" v-bind="ptm('mask')">
            <slot v-if="$slots.loading" name="loading"></slot>
            <template v-else>
                <component v-if="$slots.loadingicon" :is="$slots.loadingicon" :class="cx('loadingIcon')" />
                <i v-else-if="loadingIcon" :class="[cx('loadingIcon'), 'pi-spin', loadingIcon]" v-bind="ptm('loadingIcon')" />
                <SpinnerIcon v-else spin :class="cx('loadingIcon')" v-bind="ptm('loadingIcon')" />
            </template>
        </div>
        <div v-if="$slots.header" :class="cx('header')" v-bind="ptm('header')">
            <slot name="header"></slot>
        </div>
        <DTPaginator
            v-if="paginatorTop"
            :rows="d_rows"
            :first="d_first"
            :totalRecords="totalRecordsLength"
            :pageLinkSize="pageLinkSize"
            :template="paginatorTemplate"
            :rowsPerPageOptions="rowsPerPageOptions"
            :currentPageReportTemplate="currentPageReportTemplate"
            :class="cx('pcPaginator', { position: 'top' })"
            @page="onPage($event)"
            :alwaysShow="alwaysShowPaginator"
            :unstyled="unstyled"
            :pt="ptm('pcPaginator')"
        >
            <template v-if="$slots.paginatorcontainer" #container>
                <slot
                    name="paginatorcontainer"
                    :first="slotProps.first"
                    :last="slotProps.last"
                    :rows="slotProps.rows"
                    :page="slotProps.page"
                    :pageCount="slotProps.pageCount"
                    :totalRecords="slotProps.totalRecords"
                    :firstPageCallback="slotProps.firstPageCallback"
                    :lastPageCallback="slotProps.lastPageCallback"
                    :prevPageCallback="slotProps.prevPageCallback"
                    :nextPageCallback="slotProps.nextPageCallback"
                    :rowChangeCallback="slotProps.rowChangeCallback"
                ></slot>
            </template>
            <template v-if="$slots.paginatorstart" #start>
                <slot name="paginatorstart"></slot>
            </template>
            <template v-if="$slots.paginatorend" #end>
                <slot name="paginatorend"></slot>
            </template>
            <template v-if="$slots.paginatorfirstpagelinkicon" #firstpagelinkicon="slotProps">
                <slot name="paginatorfirstpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorprevpagelinkicon" #prevpagelinkicon="slotProps">
                <slot name="paginatorprevpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatornextpagelinkicon" #nextpagelinkicon="slotProps">
                <slot name="paginatornextpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorlastpagelinkicon" #lastpagelinkicon="slotProps">
                <slot name="paginatorlastpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorjumptopagedropdownicon" #jumptopagedropdownicon="slotProps">
                <slot name="paginatorjumptopagedropdownicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorrowsperpagedropdownicon" #rowsperpagedropdownicon="slotProps">
                <slot name="paginatorrowsperpagedropdownicon" :class="slotProps.class"></slot>
            </template>
        </DTPaginator>
        <div :class="cx('tableContainer')" :style="[sx('tableContainer'), { maxHeight: virtualScrollerDisabled ? scrollHeight : '' }]" v-bind="ptm('tableContainer')">
            <DTVirtualScroller
                ref="virtualScroller"
                v-bind="virtualScrollerOptions"
                :items="processedData"
                :columns="columns"
                :style="scrollHeight !== 'flex' ? { height: scrollHeight } : undefined"
                :scrollHeight="scrollHeight !== 'flex' ? undefined : '100%'"
                :disabled="virtualScrollerDisabled"
                loaderDisabled
                inline
                autoSize
                :showSpacer="false"
                :pt="ptm('virtualScroller')"
            >
                <template #content="slotProps">
                    <table ref="table" role="table" :class="[cx('table'), tableClass]" :style="[tableStyle, slotProps.spacerStyle]" v-bind="{ ...tableProps, ...ptm('table') }">
                        <DTTableHeader
                            v-if="showHeaders"
                            :columnGroup="headerColumnGroup"
                            :columns="slotProps.columns"
                            :rowGroupMode="rowGroupMode"
                            :groupRowsBy="groupRowsBy"
                            :groupRowSortField="groupRowSortField"
                            :reorderableColumns="reorderableColumns"
                            :resizableColumns="resizableColumns"
                            :allRowsSelected="allRowsSelected"
                            :empty="empty"
                            :sortMode="sortMode"
                            :sortField="d_sortField"
                            :sortOrder="d_sortOrder"
                            :multiSortMeta="d_multiSortMeta"
                            :filters="d_filters"
                            :filtersStore="filters"
                            :filterDisplay="filterDisplay"
                            :filterButtonProps="headerFilterButtonProps"
                            :filterInputProps="filterInputProps"
                            :first="d_first"
                            @column-click="onColumnHeaderClick($event)"
                            @column-mousedown="onColumnHeaderMouseDown($event)"
                            @filter-change="onFilterChange"
                            @filter-apply="onFilterApply"
                            @column-dragstart="onColumnHeaderDragStart($event)"
                            @column-dragover="onColumnHeaderDragOver($event)"
                            @column-dragleave="onColumnHeaderDragLeave($event)"
                            @column-drop="onColumnHeaderDrop($event)"
                            @column-resizestart="onColumnResizeStart($event)"
                            @checkbox-change="toggleRowsWithCheckbox($event)"
                            :unstyled="unstyled"
                            :pt="pt"
                        />
                        <DTTableBody
                            v-if="frozenValue"
                            ref="frozenBodyRef"
                            :value="frozenValue"
                            :frozenRow="true"
                            :columns="slotProps.columns"
                            :first="d_first"
                            :dataKey="dataKey"
                            :selection="selection"
                            :selectionKeys="d_selectionKeys"
                            :selectionMode="selectionMode"
                            :contextMenu="contextMenu"
                            :contextMenuSelection="contextMenuSelection"
                            :rowGroupMode="rowGroupMode"
                            :groupRowsBy="groupRowsBy"
                            :expandableRowGroups="expandableRowGroups"
                            :rowClass="rowClass"
                            :rowStyle="rowStyle"
                            :editMode="editMode"
                            :compareSelectionBy="compareSelectionBy"
                            :scrollable="scrollable"
                            :expandedRowIcon="expandedRowIcon"
                            :collapsedRowIcon="collapsedRowIcon"
                            :expandedRows="expandedRows"
                            :expandedRowGroups="expandedRowGroups"
                            :editingRows="editingRows"
                            :editingRowKeys="d_editingRowKeys"
                            :templates="$slots"
                            :editButtonProps="rowEditButtonProps"
                            :isVirtualScrollerDisabled="true"
                            @rowgroup-toggle="toggleRowGroup"
                            @row-click="onRowClick($event)"
                            @row-dblclick="onRowDblClick($event)"
                            @row-rightclick="onRowRightClick($event)"
                            @row-touchend="onRowTouchEnd"
                            @row-keydown="onRowKeyDown"
                            @row-mousedown="onRowMouseDown"
                            @row-dragstart="onRowDragStart($event)"
                            @row-dragover="onRowDragOver($event)"
                            @row-dragleave="onRowDragLeave($event)"
                            @row-dragend="onRowDragEnd($event)"
                            @row-drop="onRowDrop($event)"
                            @row-toggle="toggleRow($event)"
                            @radio-change="toggleRowWithRadio($event)"
                            @checkbox-change="toggleRowWithCheckbox($event)"
                            @cell-edit-init="onCellEditInit($event)"
                            @cell-edit-complete="onCellEditComplete($event)"
                            @cell-edit-cancel="onCellEditCancel($event)"
                            @row-edit-init="onRowEditInit($event)"
                            @row-edit-save="onRowEditSave($event)"
                            @row-edit-cancel="onRowEditCancel($event)"
                            :editingMeta="d_editingMeta"
                            @editing-meta-change="onEditingMetaChange"
                            :unstyled="unstyled"
                            :pt="pt"
                        />
                        <DTTableBody
                            ref="bodyRef"
                            :value="dataToRender(slotProps.rows)"
                            :class="slotProps.styleClass"
                            :columns="slotProps.columns"
                            :empty="empty"
                            :first="d_first"
                            :dataKey="dataKey"
                            :selection="selection"
                            :selectionKeys="d_selectionKeys"
                            :selectionMode="selectionMode"
                            :contextMenu="contextMenu"
                            :contextMenuSelection="contextMenuSelection"
                            :rowGroupMode="rowGroupMode"
                            :groupRowsBy="groupRowsBy"
                            :expandableRowGroups="expandableRowGroups"
                            :rowClass="rowClass"
                            :rowStyle="rowStyle"
                            :editMode="editMode"
                            :compareSelectionBy="compareSelectionBy"
                            :scrollable="scrollable"
                            :expandedRowIcon="expandedRowIcon"
                            :collapsedRowIcon="collapsedRowIcon"
                            :expandedRows="expandedRows"
                            :expandedRowGroups="expandedRowGroups"
                            :editingRows="editingRows"
                            :editingRowKeys="d_editingRowKeys"
                            :templates="$slots"
                            :editButtonProps="rowEditButtonProps"
                            :virtualScrollerContentProps="slotProps"
                            :isVirtualScrollerDisabled="virtualScrollerDisabled"
                            @rowgroup-toggle="toggleRowGroup"
                            @row-click="onRowClick($event)"
                            @row-dblclick="onRowDblClick($event)"
                            @row-rightclick="onRowRightClick($event)"
                            @row-touchend="onRowTouchEnd"
                            @row-keydown="onRowKeyDown($event, slotProps)"
                            @row-mousedown="onRowMouseDown"
                            @row-dragstart="onRowDragStart($event)"
                            @row-dragover="onRowDragOver($event)"
                            @row-dragleave="onRowDragLeave($event)"
                            @row-dragend="onRowDragEnd($event)"
                            @row-drop="onRowDrop($event)"
                            @row-toggle="toggleRow($event)"
                            @radio-change="toggleRowWithRadio($event)"
                            @checkbox-change="toggleRowWithCheckbox($event)"
                            @cell-edit-init="onCellEditInit($event)"
                            @cell-edit-complete="onCellEditComplete($event)"
                            @cell-edit-cancel="onCellEditCancel($event)"
                            @row-edit-init="onRowEditInit($event)"
                            @row-edit-save="onRowEditSave($event)"
                            @row-edit-cancel="onRowEditCancel($event)"
                            :editingMeta="d_editingMeta"
                            @editing-meta-change="onEditingMetaChange"
                            :unstyled="unstyled"
                            :pt="pt"
                        />
                        <tbody
                            v-if="hasSpacerStyle(slotProps.spacerStyle)"
                            :class="cx('virtualScrollerSpacer')"
                            :style="{ height: `calc(${slotProps.spacerStyle.height} - ${slotProps.rows.length * slotProps.itemSize}px)` }"
                            v-bind="ptm('virtualScrollerSpacer')"
                        ></tbody>
                        <DTTableFooter :columnGroup="footerColumnGroup" :columns="slotProps.columns" :pt="pt" />
                    </table>
                </template>
            </DTVirtualScroller>
        </div>
        <DTPaginator
            v-if="paginatorBottom"
            :rows="d_rows"
            :first="d_first"
            :totalRecords="totalRecordsLength"
            :pageLinkSize="pageLinkSize"
            :template="paginatorTemplate"
            :rowsPerPageOptions="rowsPerPageOptions"
            :currentPageReportTemplate="currentPageReportTemplate"
            :class="cx('pcPaginator', { position: 'bottom' })"
            @page="onPage($event)"
            :alwaysShow="alwaysShowPaginator"
            :unstyled="unstyled"
            :pt="ptm('pcPaginator')"
        >
            <template v-if="$slots.paginatorcontainer" #container="slotProps">
                <slot
                    name="paginatorcontainer"
                    :first="slotProps.first"
                    :last="slotProps.last"
                    :rows="slotProps.rows"
                    :page="slotProps.page"
                    :pageCount="slotProps.pageCount"
                    :totalRecords="slotProps.totalRecords"
                    :firstPageCallback="slotProps.firstPageCallback"
                    :lastPageCallback="slotProps.lastPageCallback"
                    :prevPageCallback="slotProps.prevPageCallback"
                    :nextPageCallback="slotProps.nextPageCallback"
                    :rowChangeCallback="slotProps.rowChangeCallback"
                ></slot>
            </template>
            <template v-if="$slots.paginatorstart" #start>
                <slot name="paginatorstart"></slot>
            </template>
            <template v-if="$slots.paginatorend" #end>
                <slot name="paginatorend"></slot>
            </template>
            <template v-if="$slots.paginatorfirstpagelinkicon" #firstpagelinkicon="slotProps">
                <slot name="paginatorfirstpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorprevpagelinkicon" #prevpagelinkicon="slotProps">
                <slot name="paginatorprevpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatornextpagelinkicon" #nextpagelinkicon="slotProps">
                <slot name="paginatornextpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorlastpagelinkicon" #lastpagelinkicon="slotProps">
                <slot name="paginatorlastpagelinkicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorjumptopagedropdownicon" #jumptopagedropdownicon="slotProps">
                <slot name="paginatorjumptopagedropdownicon" :class="slotProps.class"></slot>
            </template>
            <template v-if="$slots.paginatorrowsperpagedropdownicon" #rowsperpagedropdownicon="slotProps">
                <slot name="paginatorrowsperpagedropdownicon" :class="slotProps.class"></slot>
            </template>
        </DTPaginator>
        <div v-if="$slots.footer" :class="cx('footer')" v-bind="ptm('footer')">
            <slot name="footer"></slot>
        </div>
        <div ref="resizeHelper" :class="cx('columnResizeIndicator')" style="display: none" v-bind="ptm('columnResizeIndicator')"></div>
        <span v-if="reorderableColumns" ref="reorderIndicatorUp" :class="cx('rowReorderIndicatorUp')" style="position: absolute; display: none" v-bind="ptm('rowReorderIndicatorUp')">
            <component :is="$slots.rowreorderindicatorupicon || $slots.reorderindicatorupicon || 'ArrowDownIcon'" />
        </span>
        <span v-if="reorderableColumns" ref="reorderIndicatorDown" :class="cx('rowReorderIndicatorDown')" style="position: absolute; display: none" v-bind="ptm('rowReorderIndicatorDown')">
            <component :is="$slots.rowreorderindicatordownicon || $slots.reorderindicatordownicon || 'ArrowUpIcon'" />
        </span>
    </div>
</template>

<script>
import {
    addClass,
    addStyle,
    clearSelection,
    exportCSV,
    find,
    findSingle,
    focus,
    getAttribute,
    getHiddenElementOuterHeight,
    getHiddenElementOuterWidth,
    getIndex,
    getOffset,
    getOuterHeight,
    getOuterWidth,
    isClickable,
    isRTL,
    removeClass,
    setAttribute
} from '@primeuix/utils/dom';
import { equals, findIndexInList, isEmpty, isNotEmpty, localeComparator, reorderArray, resolveFieldData, sort } from '@primeuix/utils/object';
import { FilterMatchMode, FilterOperator, FilterService } from '@primevue/core/api';
import { HelperSet, getVNodeProp } from '@primevue/core/utils';
import ArrowDownIcon from '@primevue/icons/arrowdown';
import ArrowUpIcon from '@primevue/icons/arrowup';
import SpinnerIcon from '@primevue/icons/spinner';
import Paginator from 'primevue/paginator';
import VirtualScroller from 'primevue/virtualscroller';
import BaseDataTable from './BaseDataTable.vue';
import TableBody from './TableBody.vue';
import TableFooter from './TableFooter.vue';
import TableHeader from './TableHeader.vue';

export default {
    name: 'DataTable',
    extends: BaseDataTable,
    inheritAttrs: false,
    emits: [
        'value-change',
        'update:first',
        'update:rows',
        'page',
        'update:sortField',
        'update:sortOrder',
        'update:multiSortMeta',
        'sort',
        'filter',
        'row-click',
        'row-dblclick',
        'update:selection',
        'row-select',
        'row-unselect',
        'update:contextMenuSelection',
        'row-contextmenu',
        'row-unselect-all',
        'row-select-all',
        'select-all-change',
        'column-resize-end',
        'column-reorder',
        'row-reorder',
        'update:expandedRows',
        'row-collapse',
        'row-expand',
        'update:expandedRowGroups',
        'rowgroup-collapse',
        'rowgroup-expand',
        'update:filters',
        'state-restore',
        'state-save',
        'cell-edit-init',
        'cell-edit-complete',
        'cell-edit-cancel',
        'update:editingRows',
        'row-edit-init',
        'row-edit-save',
        'row-edit-cancel'
    ],
    provide() {
        return {
            $columns: this.d_columns,
            $columnGroups: this.d_columnGroups
        };
    },
    data() {
        return {
            d_first: this.first,
            d_rows: this.rows,
            d_sortField: this.sortField,
            d_sortOrder: this.sortOrder,
            d_nullSortOrder: this.nullSortOrder,
            d_multiSortMeta: this.multiSortMeta ? [...this.multiSortMeta] : [],
            d_groupRowsSortMeta: null,
            d_selectionKeys: null,
            d_columnOrder: null,
            d_editingRowKeys: null,
            d_editingMeta: {},
            d_filters: this.cloneFilters(this.filters),
            d_columns: new HelperSet({ type: 'Column' }),
            d_columnGroups: new HelperSet({ type: 'ColumnGroup' })
        };
    },
    rowTouched: false,
    anchorRowIndex: null,
    rangeRowIndex: null,
    documentColumnResizeListener: null,
    documentColumnResizeEndListener: null,
    lastResizeHelperX: null,
    resizeColumnElement: null,
    columnResizing: false,
    colReorderIconWidth: null,
    colReorderIconHeight: null,
    draggedColumn: null,
    draggedColumnElement: null,
    draggedRowIndex: null,
    droppedRowIndex: null,
    rowDragging: null,
    columnWidthsState: null,
    tableWidthState: null,
    columnWidthsRestored: false,
    watch: {
        first(newValue) {
            this.d_first = newValue;
        },
        rows(newValue) {
            this.d_rows = newValue;
        },
        sortField(newValue) {
            this.d_sortField = newValue;
        },
        sortOrder(newValue) {
            this.d_sortOrder = newValue;
        },
        nullSortOrder(newValue) {
            this.d_nullSortOrder = newValue;
        },
        multiSortMeta(newValue) {
            this.d_multiSortMeta = newValue;
        },
        selection: {
            immediate: true,
            handler(newValue) {
                if (this.dataKey) {
                    this.updateSelectionKeys(newValue);
                }
            }
        },
        editingRows: {
            immediate: true,
            handler(newValue) {
                if (this.dataKey) {
                    this.updateEditingRowKeys(newValue);
                }
            }
        },
        filters: {
            deep: true,
            handler: function (newValue) {
                this.d_filters = this.cloneFilters(newValue);
            }
        }
    },
    mounted() {
        if (this.isStateful()) {
            this.restoreState();

            this.resizableColumns && this.restoreColumnWidths();
        }

        if (this.editMode === 'row' && this.dataKey && !this.d_editingRowKeys) {
            this.updateEditingRowKeys(this.editingRows);
        }
    },
    beforeUnmount() {
        this.unbindColumnResizeEvents();
        this.destroyStyleElement();

        this.d_columns.clear();
        this.d_columnGroups.clear();
    },
    updated() {
        if (this.isStateful()) {
            this.saveState();
        }

        if (this.editMode === 'row' && this.dataKey && !this.d_editingRowKeys) {
            this.updateEditingRowKeys(this.editingRows);
        }
    },
    methods: {
        columnProp(col, prop) {
            return getVNodeProp(col, prop);
        },
        onPage(event) {
            this.clearEditingMetaData();

            this.d_first = event.first;
            this.d_rows = event.rows;

            let pageEvent = this.createLazyLoadEvent(event);

            pageEvent.pageCount = event.pageCount;
            pageEvent.page = event.page;

            this.$emit('update:first', this.d_first);
            this.$emit('update:rows', this.d_rows);
            this.$emit('page', pageEvent);
            this.$nextTick(() => {
                this.$emit('value-change', this.processedData);
            });
        },
        onColumnHeaderClick(e) {
            const event = e.originalEvent;
            const column = e.column;

            if (this.columnProp(column, 'sortable')) {
                const targetNode = event.target;
                const columnField = this.columnProp(column, 'sortField') || this.columnProp(column, 'field');

                if (
                    getAttribute(targetNode, 'data-p-sortable-column') === true ||
                    getAttribute(targetNode, 'data-pc-section') === 'columntitle' ||
                    getAttribute(targetNode, 'data-pc-section') === 'columnheadercontent' ||
                    getAttribute(targetNode, 'data-pc-section') === 'sorticon' ||
                    getAttribute(targetNode.parentElement, 'data-pc-section') === 'sorticon' ||
                    getAttribute(targetNode.parentElement.parentElement, 'data-pc-section') === 'sorticon' ||
                    (targetNode.closest('[data-p-sortable-column="true"]') && !targetNode.closest('[data-pc-section="columnfilterbutton"]') && !isClickable(event.target))
                ) {
                    clearSelection();

                    if (this.sortMode === 'single') {
                        if (this.d_sortField === columnField) {
                            if (this.removableSort && this.d_sortOrder * -1 === this.defaultSortOrder) {
                                this.d_sortOrder = null;
                                this.d_sortField = null;
                            } else {
                                this.d_sortOrder = this.d_sortOrder * -1;
                            }
                        } else {
                            this.d_sortOrder = this.defaultSortOrder;
                            this.d_sortField = columnField;
                        }

                        this.$emit('update:sortField', this.d_sortField);
                        this.$emit('update:sortOrder', this.d_sortOrder);
                        this.resetPage();
                    } else if (this.sortMode === 'multiple') {
                        let metaKey = event.metaKey || event.ctrlKey;

                        if (!metaKey) {
                            this.d_multiSortMeta = this.d_multiSortMeta.filter((meta) => meta.field === columnField);
                        }

                        this.addMultiSortField(columnField);
                        this.$emit('update:multiSortMeta', this.d_multiSortMeta);
                    }

                    this.$emit('sort', this.createLazyLoadEvent(event));
                    this.$nextTick(() => {
                        this.$emit('value-change', this.processedData);
                    });
                }
            }
        },
        sortSingle(value) {
            this.clearEditingMetaData();

            if (this.groupRowsBy && this.groupRowsBy === this.sortField) {
                this.d_multiSortMeta = [
                    { field: this.sortField, order: this.sortOrder || this.defaultSortOrder },
                    { field: this.d_sortField, order: this.d_sortOrder }
                ];

                return this.sortMultiple(value);
            }

            let data = [...value];
            let resolvedFieldData = new Map();

            for (let item of data) {
                resolvedFieldData.set(item, resolveFieldData(item, this.d_sortField));
            }

            const comparer = localeComparator();

            data.sort((data1, data2) => {
                let value1 = resolvedFieldData.get(data1);
                let value2 = resolvedFieldData.get(data2);

                return sort(value1, value2, this.d_sortOrder, comparer, this.d_nullSortOrder);
            });

            return data;
        },
        sortMultiple(value) {
            this.clearEditingMetaData();

            if (this.groupRowsBy && (this.d_groupRowsSortMeta || (this.d_multiSortMeta.length && this.groupRowsBy === this.d_multiSortMeta[0].field))) {
                const firstSortMeta = this.d_multiSortMeta[0];

                !this.d_groupRowsSortMeta && (this.d_groupRowsSortMeta = firstSortMeta);

                if (firstSortMeta.field !== this.d_groupRowsSortMeta.field) {
                    this.d_multiSortMeta = [this.d_groupRowsSortMeta, ...this.d_multiSortMeta];
                }
            }

            let data = [...value];

            data.sort((data1, data2) => {
                return this.multisortField(data1, data2, 0);
            });

            return data;
        },
        multisortField(data1, data2, index) {
            const value1 = resolveFieldData(data1, this.d_multiSortMeta[index].field);
            const value2 = resolveFieldData(data2, this.d_multiSortMeta[index].field);
            const comparer = localeComparator();

            if (value1 === value2) {
                return this.d_multiSortMeta.length - 1 > index ? this.multisortField(data1, data2, index + 1) : 0;
            }

            return sort(value1, value2, this.d_multiSortMeta[index].order, comparer, this.d_nullSortOrder);
        },
        addMultiSortField(field) {
            let index = this.d_multiSortMeta.findIndex((meta) => meta.field === field);

            if (index >= 0) {
                if (this.removableSort && this.d_multiSortMeta[index].order * -1 === this.defaultSortOrder) this.d_multiSortMeta.splice(index, 1);
                else this.d_multiSortMeta[index] = { field: field, order: this.d_multiSortMeta[index].order * -1 };
            } else {
                this.d_multiSortMeta.push({ field: field, order: this.defaultSortOrder });
            }

            this.d_multiSortMeta = [...this.d_multiSortMeta];
        },
        getActiveFilters(filters) {
            const removeEmptyFilters = ([key, value]) => {
                if (value.constraints) {
                    const filteredConstraints = value.constraints.filter((constraint) => constraint.value !== null);

                    if (filteredConstraints.length > 0) {
                        return [key, { ...value, constraints: filteredConstraints }];
                    }
                } else if (value.value !== null) {
                    return [key, value];
                }

                return undefined;
            };

            const filterValidEntries = (entry) => entry !== undefined;
            const entries = Object.entries(filters).map(removeEmptyFilters).filter(filterValidEntries);

            return Object.fromEntries(entries);
        },
        filter(data) {
            if (!data) {
                return;
            }

            this.clearEditingMetaData();

            let activeFilters = this.getActiveFilters(this.filters);
            let globalFilterFieldsArray;

            if (activeFilters['global']) {
                globalFilterFieldsArray = this.globalFilterFields || this.columns.map((col) => this.columnProp(col, 'filterField') || this.columnProp(col, 'field'));
            }

            let filteredValue = [];

            for (let i = 0; i < data.length; i++) {
                let localMatch = true;
                let globalMatch = false;
                let localFiltered = false;

                for (let prop in activeFilters) {
                    if (Object.prototype.hasOwnProperty.call(activeFilters, prop) && prop !== 'global') {
                        localFiltered = true;
                        let filterField = prop;
                        let filterMeta = activeFilters[filterField];

                        if (filterMeta.operator) {
                            for (let filterConstraint of filterMeta.constraints) {
                                localMatch = this.executeLocalFilter(filterField, data[i], filterConstraint);

                                if ((filterMeta.operator === FilterOperator.OR && localMatch) || (filterMeta.operator === FilterOperator.AND && !localMatch)) {
                                    break;
                                }
                            }
                        } else {
                            localMatch = this.executeLocalFilter(filterField, data[i], filterMeta);
                        }

                        if (!localMatch) {
                            break;
                        }
                    }
                }

                if (localMatch && activeFilters['global'] && !globalMatch && globalFilterFieldsArray) {
                    for (let j = 0; j < globalFilterFieldsArray.length; j++) {
                        let globalFilterField = globalFilterFieldsArray[j];

                        globalMatch = FilterService.filters[activeFilters['global'].matchMode || FilterMatchMode.CONTAINS](resolveFieldData(data[i], globalFilterField), activeFilters['global'].value, this.filterLocale);

                        if (globalMatch) {
                            break;
                        }
                    }
                }

                let matches;

                if (activeFilters['global']) {
                    matches = localFiltered ? localFiltered && localMatch && globalMatch : globalMatch;
                } else {
                    matches = localFiltered && localMatch;
                }

                if (matches) {
                    filteredValue.push(data[i]);
                }
            }

            if (filteredValue.length === this.value.length || Object.keys(activeFilters).length == 0) {
                filteredValue = data;
            }

            let filterEvent = this.createLazyLoadEvent();

            filterEvent.filteredValue = filteredValue;
            this.$emit('filter', filterEvent);
            this.$nextTick(() => {
                this.$emit('value-change', this.processedData);
            });

            return filteredValue;
        },
        executeLocalFilter(field, rowData, filterMeta) {
            let filterValue = filterMeta.value;
            let filterMatchMode = filterMeta.matchMode || FilterMatchMode.STARTS_WITH;
            let dataFieldValue = resolveFieldData(rowData, field);
            let filterConstraint = FilterService.filters[filterMatchMode];

            return filterConstraint(dataFieldValue, filterValue, this.filterLocale);
        },
        onRowClick(e) {
            const event = e.originalEvent;
            const body = this.$refs.bodyRef && this.$refs.bodyRef.$el;
            const focusedItem = findSingle(body, 'tr[data-p-selectable-row="true"][tabindex="0"]');

            if (isClickable(event.target)) {
                return;
            }

            this.$emit('row-click', e);

            if (this.selectionMode) {
                const rowData = e.data;
                const rowIndex = this.d_first + e.index;

                if (this.isMultipleSelectionMode() && event.shiftKey && this.anchorRowIndex != null) {
                    clearSelection();
                    this.rangeRowIndex = rowIndex;
                    this.selectRange(event);
                } else {
                    const selected = this.isSelected(rowData);
                    const metaSelection = this.rowTouched ? false : this.metaKeySelection;

                    this.anchorRowIndex = rowIndex;
                    this.rangeRowIndex = rowIndex;

                    if (metaSelection) {
                        let metaKey = event.metaKey || event.ctrlKey;

                        if (selected && metaKey) {
                            if (this.isSingleSelectionMode()) {
                                this.$emit('update:selection', null);
                            } else {
                                const selectionIndex = this.findIndexInSelection(rowData);
                                const _selection = this.selection.filter((val, i) => i != selectionIndex);

                                this.$emit('update:selection', _selection);
                            }

                            this.$emit('row-unselect', { originalEvent: event, data: rowData, index: rowIndex, type: 'row' });
                        } else {
                            if (this.isSingleSelectionMode()) {
                                this.$emit('update:selection', rowData);
                            } else if (this.isMultipleSelectionMode()) {
                                let _selection = metaKey ? this.selection || [] : [];

                                _selection = [..._selection, rowData];
                                this.$emit('update:selection', _selection);
                            }

                            this.$emit('row-select', { originalEvent: event, data: rowData, index: rowIndex, type: 'row' });
                        }
                    } else {
                        if (this.selectionMode === 'single') {
                            if (selected) {
                                this.$emit('update:selection', null);
                                this.$emit('row-unselect', { originalEvent: event, data: rowData, index: rowIndex, type: 'row' });
                            } else {
                                this.$emit('update:selection', rowData);
                                this.$emit('row-select', { originalEvent: event, data: rowData, index: rowIndex, type: 'row' });
                            }
                        } else if (this.selectionMode === 'multiple') {
                            if (selected) {
                                const selectionIndex = this.findIndexInSelection(rowData);
                                const _selection = this.selection.filter((val, i) => i != selectionIndex);

                                this.$emit('update:selection', _selection);
                                this.$emit('row-unselect', { originalEvent: event, data: rowData, index: rowIndex, type: 'row' });
                            } else {
                                const _selection = this.selection ? [...this.selection, rowData] : [rowData];

                                this.$emit('update:selection', _selection);
                                this.$emit('row-select', { originalEvent: event, data: rowData, index: rowIndex, type: 'row' });
                            }
                        }
                    }
                }
            }

            this.rowTouched = false;

            if (focusedItem) {
                if (event.target?.getAttribute('data-pc-section') === 'rowtoggleicon') return;

                const targetRow = event.currentTarget?.closest('tr[data-p-selectable-row="true"]');

                focusedItem.tabIndex = '-1';
                if (targetRow) targetRow.tabIndex = '0';
            }
        },
        onRowDblClick(e) {
            const event = e.originalEvent;

            if (isClickable(event.target)) {
                return;
            }

            this.$emit('row-dblclick', e);
        },
        onRowRightClick(event) {
            if (this.contextMenu) {
                clearSelection();
                event.originalEvent.target.focus();
            }

            this.$emit('update:contextMenuSelection', event.data);
            this.$emit('row-contextmenu', event);
        },
        onRowTouchEnd() {
            this.rowTouched = true;
        },
        onRowKeyDown(e, slotProps) {
            const event = e.originalEvent;
            const rowData = e.data;
            const rowIndex = e.index;
            const metaKey = event.metaKey || event.ctrlKey;

            if (this.selectionMode) {
                const row = event.target;

                switch (event.code) {
                    case 'ArrowDown':
                        this.onArrowDownKey(event, row, rowIndex, slotProps);
                        break;

                    case 'ArrowUp':
                        this.onArrowUpKey(event, row, rowIndex, slotProps);
                        break;

                    case 'Home':
                        this.onHomeKey(event, row, rowIndex, slotProps);
                        break;

                    case 'End':
                        this.onEndKey(event, row, rowIndex, slotProps);
                        break;

                    case 'Enter':
                    case 'NumpadEnter':
                        this.onEnterKey(event, rowData, rowIndex);
                        break;

                    case 'Space':
                        this.onSpaceKey(event, rowData, rowIndex, slotProps);
                        break;

                    case 'Tab':
                        this.onTabKey(event, rowIndex);
                        break;

                    default:
                        if (event.code === 'KeyA' && metaKey && this.isMultipleSelectionMode()) {
                            const data = this.dataToRender(slotProps.rows);

                            this.$emit('update:selection', data);
                        }

                        event.preventDefault();

                        break;
                }
            }
        },
        onArrowDownKey(event, row, rowIndex, slotProps) {
            const nextRow = this.findNextSelectableRow(row);

            nextRow && this.focusRowChange(row, nextRow);

            if (event.shiftKey) {
                const data = this.dataToRender(slotProps.rows);
                const nextRowIndex = rowIndex + 1 >= data.length ? data.length - 1 : rowIndex + 1;

                this.onRowClick({ originalEvent: event, data: data[nextRowIndex], index: nextRowIndex });
            }

            event.preventDefault();
        },
        onArrowUpKey(event, row, rowIndex, slotProps) {
            const prevRow = this.findPrevSelectableRow(row);

            prevRow && this.focusRowChange(row, prevRow);

            if (event.shiftKey) {
                const data = this.dataToRender(slotProps.rows);
                const prevRowIndex = rowIndex - 1 <= 0 ? 0 : rowIndex - 1;

                this.onRowClick({ originalEvent: event, data: data[prevRowIndex], index: prevRowIndex });
            }

            event.preventDefault();
        },
        onHomeKey(event, row, rowIndex, slotProps) {
            const firstRow = this.findFirstSelectableRow();

            firstRow && this.focusRowChange(row, firstRow);

            if (event.ctrlKey && event.shiftKey) {
                const data = this.dataToRender(slotProps.rows);

                this.$emit('update:selection', data.slice(0, rowIndex + 1));
            }

            event.preventDefault();
        },
        onEndKey(event, row, rowIndex, slotProps) {
            const lastRow = this.findLastSelectableRow();

            lastRow && this.focusRowChange(row, lastRow);

            if (event.ctrlKey && event.shiftKey) {
                const data = this.dataToRender(slotProps.rows);

                this.$emit('update:selection', data.slice(rowIndex, data.length));
            }

            event.preventDefault();
        },
        onEnterKey(event, rowData, rowIndex) {
            this.onRowClick({ originalEvent: event, data: rowData, index: rowIndex });
            event.preventDefault();
        },
        onSpaceKey(event, rowData, rowIndex, slotProps) {
            this.onEnterKey(event, rowData, rowIndex);

            if (event.shiftKey && this.selection !== null) {
                const data = this.dataToRender(slotProps.rows);
                let index;

                if (this.selection.length > 0) {
                    let firstSelectedRowIndex, lastSelectedRowIndex;

                    firstSelectedRowIndex = findIndexInList(this.selection[0], data);
                    lastSelectedRowIndex = findIndexInList(this.selection[this.selection.length - 1], data);

                    index = rowIndex <= firstSelectedRowIndex ? lastSelectedRowIndex : firstSelectedRowIndex;
                } else {
                    index = findIndexInList(this.selection, data);
                }

                const _selection = index !== rowIndex ? data.slice(Math.min(index, rowIndex), Math.max(index, rowIndex) + 1) : rowData;

                this.$emit('update:selection', _selection);
            }
        },
        onTabKey(event, rowIndex) {
            const body = this.$refs.bodyRef && this.$refs.bodyRef.$el;
            const rows = find(body, 'tr[data-p-selectable-row="true"]');

            if (event.code === 'Tab' && rows && rows.length > 0) {
                const firstSelectedRow = findSingle(body, 'tr[data-p-selected="true"]');
                const focusedItem = findSingle(body, 'tr[data-p-selectable-row="true"][tabindex="0"]');

                if (firstSelectedRow) {
                    firstSelectedRow.tabIndex = '0';
                    focusedItem && focusedItem !== firstSelectedRow && (focusedItem.tabIndex = '-1');
                } else {
                    rows[0].tabIndex = '0';
                    focusedItem !== rows[0] && (rows[rowIndex].tabIndex = '-1');
                }
            }
        },
        findNextSelectableRow(row) {
            let nextRow = row.nextElementSibling;

            if (nextRow) {
                if (getAttribute(nextRow, 'data-p-selectable-row') === true) return nextRow;
                else return this.findNextSelectableRow(nextRow);
            } else {
                return null;
            }
        },
        findPrevSelectableRow(row) {
            let prevRow = row.previousElementSibling;

            if (prevRow) {
                if (getAttribute(prevRow, 'data-p-selectable-row') === true) return prevRow;
                else return this.findPrevSelectableRow(prevRow);
            } else {
                return null;
            }
        },
        findFirstSelectableRow() {
            const firstRow = findSingle(this.$refs.table, 'tr[data-p-selectable-row="true"]');

            return firstRow;
        },
        findLastSelectableRow() {
            const rows = find(this.$refs.table, 'tr[data-p-selectable-row="true"]');

            return rows ? rows[rows.length - 1] : null;
        },
        focusRowChange(firstFocusableRow, currentFocusedRow) {
            firstFocusableRow.tabIndex = '-1';
            currentFocusedRow.tabIndex = '0';
            focus(currentFocusedRow);
        },
        toggleRowWithRadio(event) {
            const rowData = event.data;

            if (this.isSelected(rowData)) {
                this.$emit('update:selection', null);
                this.$emit('row-unselect', { originalEvent: event.originalEvent, data: rowData, index: event.index, type: 'radiobutton' });
            } else {
                this.$emit('update:selection', rowData);
                this.$emit('row-select', { originalEvent: event.originalEvent, data: rowData, index: event.index, type: 'radiobutton' });
            }
        },
        toggleRowWithCheckbox(event) {
            const rowData = event.data;

            if (this.isSelected(rowData)) {
                const selectionIndex = this.findIndexInSelection(rowData);
                const _selection = this.selection.filter((val, i) => i != selectionIndex);

                this.$emit('update:selection', _selection);
                this.$emit('row-unselect', { originalEvent: event.originalEvent, data: rowData, index: event.index, type: 'checkbox' });
            } else {
                let _selection = this.selection ? [...this.selection] : [];

                _selection = [..._selection, rowData];
                this.$emit('update:selection', _selection);
                this.$emit('row-select', { originalEvent: event.originalEvent, data: rowData, index: event.index, type: 'checkbox' });
            }
        },
        toggleRowsWithCheckbox(event) {
            if (this.selectAll !== null) {
                this.$emit('select-all-change', event);
            } else {
                const { originalEvent, checked } = event;
                let _selection = [];

                if (checked) {
                    _selection = this.frozenValue ? [...this.frozenValue, ...this.processedData] : this.processedData;
                    this.$emit('row-select-all', { originalEvent, data: _selection });
                } else {
                    this.$emit('row-unselect-all', { originalEvent });
                }

                this.$emit('update:selection', _selection);
            }
        },
        isSingleSelectionMode() {
            return this.selectionMode === 'single';
        },
        isMultipleSelectionMode() {
            return this.selectionMode === 'multiple';
        },
        isSelected(rowData) {
            if (rowData && this.selection) {
                if (this.dataKey) {
                    return this.d_selectionKeys ? this.d_selectionKeys[resolveFieldData(rowData, this.dataKey)] !== undefined : false;
                } else {
                    if (this.selection instanceof Array) return this.findIndexInSelection(rowData) > -1;
                    else return this.equals(rowData, this.selection);
                }
            }

            return false;
        },
        findIndexInSelection(rowData) {
            return this.findIndex(rowData, this.selection);
        },
        findIndex(rowData, collection) {
            let index = -1;

            if (collection && collection.length) {
                for (let i = 0; i < collection.length; i++) {
                    if (this.equals(rowData, collection[i])) {
                        index = i;
                        break;
                    }
                }
            }

            return index;
        },
        updateSelectionKeys(selection) {
            this.d_selectionKeys = {};

            if (Array.isArray(selection)) {
                for (let data of selection) {
                    this.d_selectionKeys[String(resolveFieldData(data, this.dataKey))] = 1;
                }
            } else {
                this.d_selectionKeys[String(resolveFieldData(selection, this.dataKey))] = 1;
            }
        },
        updateEditingRowKeys(editingRows) {
            if (editingRows && editingRows.length) {
                this.d_editingRowKeys = {};

                for (let data of editingRows) {
                    this.d_editingRowKeys[String(resolveFieldData(data, this.dataKey))] = 1;
                }
            } else {
                this.d_editingRowKeys = null;
            }
        },
        equals(data1, data2) {
            return this.compareSelectionBy === 'equals' ? data1 === data2 : equals(data1, data2, this.dataKey);
        },
        selectRange(event) {
            let rangeStart, rangeEnd;

            if (this.rangeRowIndex > this.anchorRowIndex) {
                rangeStart = this.anchorRowIndex;
                rangeEnd = this.rangeRowIndex;
            } else if (this.rangeRowIndex < this.anchorRowIndex) {
                rangeStart = this.rangeRowIndex;
                rangeEnd = this.anchorRowIndex;
            } else {
                rangeStart = this.rangeRowIndex;
                rangeEnd = this.rangeRowIndex;
            }

            if (this.lazy && this.paginator) {
                rangeStart -= this.first;
                rangeEnd -= this.first;
            }

            const value = this.processedData;
            let _selection = [];

            for (let i = rangeStart; i <= rangeEnd; i++) {
                let rangeRowData = value[i];

                _selection.push(rangeRowData);
                this.$emit('row-select', { originalEvent: event, data: rangeRowData, type: 'row' });
            }

            this.$emit('update:selection', _selection);
        },
        exportCSV(options, data) {
            let csv = '\ufeff';

            if (!data) {
                data = this.processedData;

                if (options && options.selectionOnly) data = this.selection || [];
                else if (this.frozenValue) data = data ? [...this.frozenValue, ...data] : this.frozenValue;
            }

            //headers
            let headerInitiated = false;

            for (let i = 0; i < this.columns.length; i++) {
                let column = this.columns[i];

                if (this.columnProp(column, 'exportable') !== false && this.columnProp(column, 'field')) {
                    if (headerInitiated) csv += this.csvSeparator;
                    else headerInitiated = true;

                    csv += '"' + (this.columnProp(column, 'exportHeader') || this.columnProp(column, 'header') || this.columnProp(column, 'field')) + '"';
                }
            }

            //body
            if (data) {
                data.forEach((record) => {
                    csv += '\n';
                    let rowInitiated = false;

                    for (let i = 0; i < this.columns.length; i++) {
                        let column = this.columns[i];

                        if (this.columnProp(column, 'exportable') !== false && this.columnProp(column, 'field')) {
                            if (rowInitiated) csv += this.csvSeparator;
                            else rowInitiated = true;

                            let cellData = resolveFieldData(record, this.columnProp(column, 'field'));

                            if (cellData != null) {
                                if (this.exportFunction) {
                                    cellData = this.exportFunction({
                                        data: cellData,
                                        field: this.columnProp(column, 'field')
                                    });
                                } else cellData = String(cellData).replace(/"/g, '""');
                            } else cellData = '';

                            csv += '"' + cellData + '"';
                        }
                    }
                });
            }

            //footers
            let footerInitiated = false;

            for (let i = 0; i < this.columns.length; i++) {
                let column = this.columns[i];

                if (i === 0) csv += '\n';

                if (this.columnProp(column, 'exportable') !== false && this.columnProp(column, 'exportFooter')) {
                    if (footerInitiated) csv += this.csvSeparator;
                    else footerInitiated = true;

                    csv += '"' + (this.columnProp(column, 'exportFooter') || this.columnProp(column, 'footer') || this.columnProp(column, 'field')) + '"';
                }
            }

            exportCSV(csv, this.exportFilename);
        },
        resetPage() {
            this.d_first = 0;
            this.$emit('update:first', this.d_first);
        },
        onColumnResizeStart(event) {
            let containerLeft = getOffset(this.$el).left;

            this.resizeColumnElement = event.target.parentElement;
            this.columnResizing = true;
            this.lastResizeHelperX = event.pageX - containerLeft + this.$el.scrollLeft;

            this.bindColumnResizeEvents();
        },
        onColumnResize(event) {
            let containerLeft = getOffset(this.$el).left;

            this.$el.setAttribute('data-p-unselectable-text', 'true');
            !this.isUnstyled && addStyle(this.$el, { 'user-select': 'none' });
            this.$refs.resizeHelper.style.height = this.$el.offsetHeight + 'px';
            this.$refs.resizeHelper.style.top = 0 + 'px';
            this.$refs.resizeHelper.style.left = event.pageX - containerLeft + this.$el.scrollLeft + 'px';

            this.$refs.resizeHelper.style.display = 'block';
        },
        onColumnResizeEnd() {
            let delta = isRTL(this.$el) ? this.lastResizeHelperX - this.$refs.resizeHelper.offsetLeft : this.$refs.resizeHelper.offsetLeft - this.lastResizeHelperX;
            let columnWidth = this.resizeColumnElement.offsetWidth;
            let newColumnWidth = columnWidth + delta;
            let minWidth = this.resizeColumnElement.style.minWidth || 15;

            if (columnWidth + delta > parseInt(minWidth, 10)) {
                if (this.columnResizeMode === 'fit') {
                    let nextColumn = this.resizeColumnElement.nextElementSibling;
                    let nextColumnWidth = nextColumn.offsetWidth - delta;

                    if (newColumnWidth > 15 && nextColumnWidth > 15) {
                        this.resizeTableCells(newColumnWidth, nextColumnWidth);
                    }
                } else if (this.columnResizeMode === 'expand') {
                    const tableWidth = this.$refs.table.offsetWidth + delta + 'px';

                    const updateTableWidth = (el) => {
                        !!el && (el.style.width = el.style.minWidth = tableWidth);
                    };

                    // Reasoning: resize table cells before updating the table width so that it can use existing computed cell widths and adjust only the one column.
                    this.resizeTableCells(newColumnWidth);
                    updateTableWidth(this.$refs.table);

                    if (!this.virtualScrollerDisabled) {
                        const body = this.$refs.bodyRef && this.$refs.bodyRef.$el;
                        const frozenBody = this.$refs.frozenBodyRef && this.$refs.frozenBodyRef.$el;

                        updateTableWidth(body);
                        updateTableWidth(frozenBody);
                    }
                }

                this.$emit('column-resize-end', {
                    element: this.resizeColumnElement,
                    delta: delta
                });
            }

            this.$refs.resizeHelper.style.display = 'none';
            this.resizeColumn = null;
            this.$el.removeAttribute('data-p-unselectable-text');
            !this.isUnstyled && (this.$el.style['user-select'] = '');

            this.unbindColumnResizeEvents();

            if (this.isStateful()) {
                this.saveState();
            }
        },
        resizeTableCells(newColumnWidth, nextColumnWidth) {
            let colIndex = getIndex(this.resizeColumnElement);
            let widths = [];
            let headers = find(this.$refs.table, 'thead[data-pc-section="thead"] > tr > th');

            headers.forEach((header) => widths.push(getOuterWidth(header)));

            this.destroyStyleElement();
            this.createStyleElement();

            let innerHTML = '';
            let selector = `[data-pc-name="datatable"][${this.$attrSelector}] > [data-pc-section="tablecontainer"] ${this.virtualScrollerDisabled ? '' : '> [data-pc-name="virtualscroller"]'} > table[data-pc-section="table"]`;

            widths.forEach((width, index) => {
                let colWidth = index === colIndex ? newColumnWidth : nextColumnWidth && index === colIndex + 1 ? nextColumnWidth : width;
                let style = `width: ${colWidth}px !important; max-width: ${colWidth}px !important`;

                innerHTML += `
                    ${selector} > thead[data-pc-section="thead"] > tr > th:nth-child(${index + 1}),
                    ${selector} > tbody[data-pc-section="tbody"] > tr > td:nth-child(${index + 1}),
                    ${selector} > tfoot[data-pc-section="tfoot"] > tr > td:nth-child(${index + 1}) {
                        ${style}
                    }
                `;
            });

            this.styleElement.innerHTML = innerHTML;
        },
        bindColumnResizeEvents() {
            if (!this.documentColumnResizeListener) {
                this.documentColumnResizeListener = document.addEventListener('mousemove', () => {
                    if (this.columnResizing) {
                        this.onColumnResize(event);
                    }
                });
            }

            if (!this.documentColumnResizeEndListener) {
                this.documentColumnResizeEndListener = document.addEventListener('mouseup', () => {
                    if (this.columnResizing) {
                        this.columnResizing = false;
                        this.onColumnResizeEnd();
                    }
                });
            }
        },
        unbindColumnResizeEvents() {
            if (this.documentColumnResizeListener) {
                document.removeEventListener('document', this.documentColumnResizeListener);
                this.documentColumnResizeListener = null;
            }

            if (this.documentColumnResizeEndListener) {
                document.removeEventListener('document', this.documentColumnResizeEndListener);
                this.documentColumnResizeEndListener = null;
            }
        },
        onColumnHeaderMouseDown(e) {
            const event = e.originalEvent;
            const column = e.column;

            if (this.reorderableColumns && this.columnProp(column, 'reorderableColumn') !== false) {
                if (event.target.nodeName === 'INPUT' || event.target.nodeName === 'TEXTAREA' || getAttribute(event.target, '[data-pc-section="columnresizer"]')) event.currentTarget.draggable = false;
                else event.currentTarget.draggable = true;
            }
        },
        onColumnHeaderDragStart(e) {
            const { originalEvent: event, column } = e;

            if (this.columnResizing) {
                event.preventDefault();

                return;
            }

            this.colReorderIconWidth = getHiddenElementOuterWidth(this.$refs.reorderIndicatorUp);
            this.colReorderIconHeight = getHiddenElementOuterHeight(this.$refs.reorderIndicatorUp);

            this.draggedColumn = column;
            this.draggedColumnElement = this.findParentHeader(event.target);
            event.dataTransfer.setData('text', 'b'); // Firefox requires this to make dragging possible
        },
        onColumnHeaderDragOver(e) {
            const { originalEvent: event, column } = e;
            let dropHeader = this.findParentHeader(event.target);

            if (this.reorderableColumns && this.draggedColumnElement && dropHeader && !this.columnProp(column, 'frozen')) {
                event.preventDefault();
                let containerOffset = getOffset(this.$el);
                let dropHeaderOffset = getOffset(dropHeader);

                if (this.draggedColumnElement !== dropHeader) {
                    let targetLeft = dropHeaderOffset.left - containerOffset.left;
                    let columnCenter = dropHeaderOffset.left + dropHeader.offsetWidth / 2;

                    this.$refs.reorderIndicatorUp.style.top = dropHeaderOffset.top - containerOffset.top - (this.colReorderIconHeight - 1) + 'px';
                    this.$refs.reorderIndicatorDown.style.top = dropHeaderOffset.top - containerOffset.top + dropHeader.offsetHeight + 'px';

                    if (event.pageX > columnCenter) {
                        this.$refs.reorderIndicatorUp.style.left = targetLeft + dropHeader.offsetWidth - Math.ceil(this.colReorderIconWidth / 2) + 'px';
                        this.$refs.reorderIndicatorDown.style.left = targetLeft + dropHeader.offsetWidth - Math.ceil(this.colReorderIconWidth / 2) + 'px';
                        this.dropPosition = 1;
                    } else {
                        this.$refs.reorderIndicatorUp.style.left = targetLeft - Math.ceil(this.colReorderIconWidth / 2) + 'px';
                        this.$refs.reorderIndicatorDown.style.left = targetLeft - Math.ceil(this.colReorderIconWidth / 2) + 'px';
                        this.dropPosition = -1;
                    }

                    this.$refs.reorderIndicatorUp.style.display = 'block';
                    this.$refs.reorderIndicatorDown.style.display = 'block';
                }
            }
        },
        onColumnHeaderDragLeave(e) {
            const { originalEvent: event } = e;

            if (this.reorderableColumns && this.draggedColumnElement) {
                event.preventDefault();
                this.$refs.reorderIndicatorUp.style.display = 'none';
                this.$refs.reorderIndicatorDown.style.display = 'none';
            }
        },
        onColumnHeaderDrop(e) {
            const { originalEvent: event, column } = e;

            event.preventDefault();

            if (this.draggedColumnElement) {
                let dragIndex = getIndex(this.draggedColumnElement);
                let dropIndex = getIndex(this.findParentHeader(event.target));
                let allowDrop = dragIndex !== dropIndex;

                if (allowDrop && ((dropIndex - dragIndex === 1 && this.dropPosition === -1) || (dropIndex - dragIndex === -1 && this.dropPosition === 1))) {
                    allowDrop = false;
                }

                if (allowDrop) {
                    let isSameColumn = (col1, col2) =>
                        this.columnProp(col1, 'columnKey') || this.columnProp(col2, 'columnKey') ? this.columnProp(col1, 'columnKey') === this.columnProp(col2, 'columnKey') : this.columnProp(col1, 'field') === this.columnProp(col2, 'field');
                    let dragColIndex = this.columns.findIndex((child) => isSameColumn(child, this.draggedColumn));
                    let dropColIndex = this.columns.findIndex((child) => isSameColumn(child, column));
                    let widths = [];
                    let headers = find(this.$el, 'thead[data-pc-section="thead"] > tr > th');

                    headers.forEach((header) => widths.push(getOuterWidth(header)));
                    const movedItem = widths.find((_, index) => index === dragColIndex);
                    const remainingItems = widths.filter((_, index) => index !== dragColIndex);
                    const reorderedWidths = [...remainingItems.slice(0, dropColIndex), movedItem, ...remainingItems.slice(dropColIndex)];

                    this.addColumnWidthStyles(reorderedWidths);

                    if (dropColIndex < dragColIndex && this.dropPosition === 1) {
                        dropColIndex++;
                    }

                    if (dropColIndex > dragColIndex && this.dropPosition === -1) {
                        dropColIndex--;
                    }

                    reorderArray(this.columns, dragColIndex, dropColIndex);
                    this.updateReorderableColumns();

                    this.$emit('column-reorder', {
                        originalEvent: event,
                        dragIndex: dragColIndex,
                        dropIndex: dropColIndex
                    });
                }

                this.$refs.reorderIndicatorUp.style.display = 'none';
                this.$refs.reorderIndicatorDown.style.display = 'none';
                this.draggedColumnElement.draggable = false;
                this.draggedColumnElement = null;
                this.draggedColumn = null;
                this.dropPosition = null;
            }
        },
        findParentHeader(element) {
            if (element.nodeName === 'TH') {
                return element;
            } else {
                let parent = element.parentElement;

                while (parent.nodeName !== 'TH') {
                    parent = parent.parentElement;
                    if (!parent) break;
                }

                return parent;
            }
        },
        findColumnByKey(columns, key) {
            if (columns && columns.length) {
                for (let i = 0; i < columns.length; i++) {
                    let column = columns[i];

                    if (this.columnProp(column, 'columnKey') === key || this.columnProp(column, 'field') === key) {
                        return column;
                    }
                }
            }

            return null;
        },
        onRowMouseDown(event) {
            if (getAttribute(event.target, 'data-pc-section') === 'reorderablerowhandle' || getAttribute(event.target.parentElement, 'data-pc-section') === 'reorderablerowhandle') event.currentTarget.draggable = true;
            else event.currentTarget.draggable = false;
        },
        onRowDragStart(e) {
            const event = e.originalEvent;
            const index = e.index;

            this.rowDragging = true;
            this.draggedRowIndex = index;
            event.dataTransfer.setData('text', 'b'); // For firefox
        },
        onRowDragOver(e) {
            const event = e.originalEvent;
            const index = e.index;

            if (this.rowDragging && this.draggedRowIndex !== index) {
                let rowElement = event.currentTarget;
                let rowY = getOffset(rowElement).top;
                let pageY = event.pageY;
                let rowMidY = rowY + getOuterHeight(rowElement) / 2;
                let prevRowElement = rowElement.previousElementSibling;

                if (pageY < rowMidY) {
                    rowElement.setAttribute('data-p-datatable-dragpoint-bottom', 'false');
                    !this.isUnstyled && removeClass(rowElement, 'p-datatable-dragpoint-bottom');

                    this.droppedRowIndex = index;

                    if (prevRowElement) {
                        prevRowElement.setAttribute('data-p-datatable-dragpoint-bottom', 'true');
                        !this.isUnstyled && addClass(prevRowElement, 'p-datatable-dragpoint-bottom');
                    } else {
                        rowElement.setAttribute('data-p-datatable-dragpoint-top', 'true');
                        !this.isUnstyled && addClass(rowElement, 'p-datatable-dragpoint-top');
                    }
                } else {
                    if (prevRowElement) {
                        prevRowElement.setAttribute('data-p-datatable-dragpoint-bottom', 'false');
                        !this.isUnstyled && removeClass(prevRowElement, 'p-datatable-dragpoint-bottom');
                    } else {
                        rowElement.setAttribute('data-p-datatable-dragpoint-top', 'true');
                        !this.isUnstyled && addClass(rowElement, 'p-datatable-dragpoint-top');
                    }

                    this.droppedRowIndex = index + 1;
                    rowElement.setAttribute('data-p-datatable-dragpoint-bottom', 'true');
                    !this.isUnstyled && addClass(rowElement, 'p-datatable-dragpoint-bottom');
                }

                event.preventDefault();
            }
        },
        onRowDragLeave(event) {
            let rowElement = event.currentTarget;
            let prevRowElement = rowElement.previousElementSibling;

            if (prevRowElement) {
                prevRowElement.setAttribute('data-p-datatable-dragpoint-bottom', 'false');
                !this.isUnstyled && removeClass(prevRowElement, 'p-datatable-dragpoint-bottom');
            }

            rowElement.setAttribute('data-p-datatable-dragpoint-bottom', 'false');
            !this.isUnstyled && removeClass(rowElement, 'p-datatable-dragpoint-bottom');
            rowElement.setAttribute('data-p-datatable-dragpoint-top', 'false');
            !this.isUnstyled && removeClass(rowElement, 'p-datatable-dragpoint-top');
        },
        onRowDragEnd(event) {
            this.rowDragging = false;
            this.draggedRowIndex = null;
            this.droppedRowIndex = null;
            event.currentTarget.draggable = false;
        },
        onRowDrop(event) {
            if (this.droppedRowIndex != null) {
                let dropIndex = this.draggedRowIndex > this.droppedRowIndex ? this.droppedRowIndex : this.droppedRowIndex === 0 ? 0 : this.droppedRowIndex - 1;
                let processedData = [...this.processedData];

                reorderArray(processedData, this.draggedRowIndex + this.d_first, dropIndex + this.d_first);

                this.$emit('row-reorder', {
                    originalEvent: event,
                    dragIndex: this.draggedRowIndex,
                    dropIndex: dropIndex,
                    value: processedData
                });
            }

            //cleanup
            this.onRowDragLeave(event);
            this.onRowDragEnd(event);
            event.preventDefault();
        },
        toggleRow(event) {
            const { expanded, ...rest } = event;
            const rowData = event.data;
            let expandedRows;

            if (this.dataKey) {
                const value = resolveFieldData(rowData, this.dataKey);

                expandedRows = this.expandedRows ? { ...this.expandedRows } : {};
                expanded ? (expandedRows[value] = true) : delete expandedRows[value];
            } else {
                expandedRows = this.expandedRows ? [...this.expandedRows] : [];
                expanded ? expandedRows.push(rowData) : (expandedRows = expandedRows.filter((d) => !this.equals(rowData, d)));
            }

            this.$emit('update:expandedRows', expandedRows);
            expanded ? this.$emit('row-expand', rest) : this.$emit('row-collapse', rest);
        },
        toggleRowGroup(e) {
            const event = e.originalEvent;
            const data = e.data;
            const groupFieldValue = resolveFieldData(data, this.groupRowsBy);
            let _expandedRowGroups = this.expandedRowGroups ? [...this.expandedRowGroups] : [];

            if (this.isRowGroupExpanded(data)) {
                _expandedRowGroups = _expandedRowGroups.filter((group) => group !== groupFieldValue);
                this.$emit('update:expandedRowGroups', _expandedRowGroups);
                this.$emit('rowgroup-collapse', { originalEvent: event, data: groupFieldValue });
            } else {
                _expandedRowGroups.push(groupFieldValue);
                this.$emit('update:expandedRowGroups', _expandedRowGroups);
                this.$emit('rowgroup-expand', { originalEvent: event, data: groupFieldValue });
            }
        },
        isRowGroupExpanded(rowData) {
            if (this.expandableRowGroups && this.expandedRowGroups) {
                let groupFieldValue = resolveFieldData(rowData, this.groupRowsBy);

                return this.expandedRowGroups.indexOf(groupFieldValue) > -1;
            }

            return false;
        },
        isStateful() {
            return this.stateKey != null;
        },
        getStorage() {
            switch (this.stateStorage) {
                case 'local':
                    return window.localStorage;

                case 'session':
                    return window.sessionStorage;

                default:
                    throw new Error(this.stateStorage + ' is not a valid value for the state storage, supported values are "local" and "session".');
            }
        },
        saveState() {
            const storage = this.getStorage();
            let state = {};

            if (this.paginator) {
                state.first = this.d_first;
                state.rows = this.d_rows;
            }

            if (this.d_sortField) {
                state.sortField = this.d_sortField;
                state.sortOrder = this.d_sortOrder;
            }

            if (this.d_multiSortMeta) {
                state.multiSortMeta = this.d_multiSortMeta;
            }

            if (this.hasFilters) {
                state.filters = this.filters;
            }

            if (this.resizableColumns) {
                this.saveColumnWidths(state);
            }

            if (this.reorderableColumns) {
                state.columnOrder = this.d_columnOrder;
            }

            if (this.expandedRows) {
                state.expandedRows = this.expandedRows;
            }

            if (this.expandedRowGroups) {
                state.expandedRowGroups = this.expandedRowGroups;
            }

            if (this.selection) {
                state.selection = this.selection;
                state.selectionKeys = this.d_selectionKeys;
            }

            if (Object.keys(state).length) {
                storage.setItem(this.stateKey, JSON.stringify(state));
            }

            this.$emit('state-save', state);
        },
        restoreState() {
            const storage = this.getStorage();
            const stateString = storage.getItem(this.stateKey);
            const dateFormat = /\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{3}Z/;

            const reviver = function (key, value) {
                if (typeof value === 'string' && dateFormat.test(value)) {
                    return new Date(value);
                }

                return value;
            };

            if (stateString) {
                let restoredState = JSON.parse(stateString, reviver);

                if (this.paginator) {
                    this.d_first = restoredState.first;
                    this.d_rows = restoredState.rows;
                }

                if (restoredState.sortField) {
                    this.d_sortField = restoredState.sortField;
                    this.d_sortOrder = restoredState.sortOrder;
                }

                if (restoredState.multiSortMeta) {
                    this.d_multiSortMeta = restoredState.multiSortMeta;
                }

                if (restoredState.filters) {
                    this.$emit('update:filters', restoredState.filters);
                }

                if (this.resizableColumns) {
                    this.columnWidthsState = restoredState.columnWidths;
                    this.tableWidthState = restoredState.tableWidth;
                }

                if (this.reorderableColumns) {
                    this.d_columnOrder = restoredState.columnOrder;
                }

                if (restoredState.expandedRows) {
                    this.$emit('update:expandedRows', restoredState.expandedRows);
                }

                if (restoredState.expandedRowGroups) {
                    this.$emit('update:expandedRowGroups', restoredState.expandedRowGroups);
                }

                if (restoredState.selection) {
                    this.d_selectionKeys = restoredState.d_selectionKeys;
                    this.$emit('update:selection', restoredState.selection);
                }

                this.$emit('state-restore', restoredState);
            }
        },
        saveColumnWidths(state) {
            let widths = [];
            let headers = find(this.$el, 'thead[data-pc-section="thead"] > tr > th');

            headers.forEach((header) => widths.push(getOuterWidth(header)));
            state.columnWidths = widths.join(',');

            if (this.columnResizeMode === 'expand') {
                state.tableWidth = getOuterWidth(this.$refs.table) + 'px';
            }
        },
        addColumnWidthStyles(widths) {
            this.createStyleElement();

            let innerHTML = '';
            let selector = `[data-pc-name="datatable"][${this.$attrSelector}] > [data-pc-section="tablecontainer"] ${this.virtualScrollerDisabled ? '' : '> [data-pc-name="virtualscroller"]'} > table[data-pc-section="table"]`;

            widths.forEach((width, index) => {
                let style = `width: ${width}px !important; max-width: ${width}px !important`;

                innerHTML += `
        ${selector} > thead[data-pc-section="thead"] > tr > th:nth-child(${index + 1}),
        ${selector} > tbody[data-pc-section="tbody"] > tr > td:nth-child(${index + 1}),
        ${selector} > tfoot[data-pc-section="tfoot"] > tr > td:nth-child(${index + 1}) {
            ${style}
        }
    `;
            });

            this.styleElement.innerHTML = innerHTML;
        },
        restoreColumnWidths() {
            if (this.columnWidthsState) {
                let widths = this.columnWidthsState.split(',');

                if (this.columnResizeMode === 'expand' && this.tableWidthState) {
                    this.$refs.table.style.width = this.tableWidthState;
                    this.$refs.table.style.minWidth = this.tableWidthState;
                }

                if (isNotEmpty(widths)) {
                    this.addColumnWidthStyles(widths);
                }
            }
        },
        onCellEditInit(event) {
            this.$emit('cell-edit-init', event);
        },
        onCellEditComplete(event) {
            this.$emit('cell-edit-complete', event);
        },
        onCellEditCancel(event) {
            this.$emit('cell-edit-cancel', event);
        },
        onRowEditInit(event) {
            let _editingRows = this.editingRows ? [...this.editingRows] : [];

            _editingRows.push(event.data);
            this.$emit('update:editingRows', _editingRows);
            this.$emit('row-edit-init', event);
        },
        onRowEditSave(event) {
            let _editingRows = [...this.editingRows];

            _editingRows.splice(this.findIndex(event.data, _editingRows), 1);
            this.$emit('update:editingRows', _editingRows);
            this.$emit('row-edit-save', event);
        },
        onRowEditCancel(event) {
            let _editingRows = [...this.editingRows];

            _editingRows.splice(this.findIndex(event.data, _editingRows), 1);
            this.$emit('update:editingRows', _editingRows);
            this.$emit('row-edit-cancel', event);
        },
        onEditingMetaChange(event) {
            let { data, field, index, editing } = event;
            let editingMeta = { ...this.d_editingMeta };
            let meta = editingMeta[index];

            if (editing) {
                !meta && (meta = editingMeta[index] = { data: { ...data }, fields: [] });
                meta['fields'].push(field);
            } else if (meta) {
                const fields = meta['fields'].filter((f) => f !== field);

                !fields.length ? delete editingMeta[index] : (meta['fields'] = fields);
            }

            this.d_editingMeta = editingMeta;
        },
        clearEditingMetaData() {
            if (this.editMode) {
                this.d_editingMeta = {};
            }
        },
        createLazyLoadEvent(event) {
            return {
                originalEvent: event,
                first: this.d_first,
                rows: this.d_rows,
                sortField: this.d_sortField,
                sortOrder: this.d_sortOrder,
                multiSortMeta: this.d_multiSortMeta,
                filters: this.d_filters
            };
        },
        hasGlobalFilter() {
            return this.filters && Object.prototype.hasOwnProperty.call(this.filters, 'global');
        },
        onFilterChange(filters) {
            this.d_filters = filters;
        },
        onFilterApply() {
            this.d_first = 0;
            this.$emit('update:first', this.d_first);
            this.$emit('update:filters', this.d_filters);

            if (this.lazy) {
                this.$emit('filter', this.createLazyLoadEvent());
            }
        },
        cloneFilters() {
            let cloned = {};

            if (this.filters) {
                Object.entries(this.filters).forEach(([prop, value]) => {
                    cloned[prop] = value.operator
                        ? {
                              operator: value.operator,
                              constraints: value.constraints.map((constraint) => {
                                  return { ...constraint };
                              })
                          }
                        : { ...value };
                });
            }

            return cloned;
        },
        updateReorderableColumns() {
            let columnOrder = [];

            this.columns.forEach((col) => columnOrder.push(this.columnProp(col, 'columnKey') || this.columnProp(col, 'field')));
            this.d_columnOrder = columnOrder;
        },
        createStyleElement() {
            this.styleElement = document.createElement('style');
            this.styleElement.type = 'text/css';
            setAttribute(this.styleElement, 'nonce', this.$primevue?.config?.csp?.nonce);
            document.head.appendChild(this.styleElement);
        },
        destroyStyleElement() {
            if (this.styleElement) {
                document.head.removeChild(this.styleElement);
                this.styleElement = null;
            }
        },
        dataToRender(data) {
            const _data = data || this.processedData;

            if (_data && this.paginator) {
                const first = this.lazy ? 0 : this.d_first;

                return _data.slice(first, first + this.d_rows);
            }

            return _data;
        },
        getVirtualScrollerRef() {
            return this.$refs.virtualScroller;
        },
        hasSpacerStyle(style) {
            return isNotEmpty(style);
        }
    },
    computed: {
        columns() {
            const cols = this.d_columns.get(this);

            if (this.reorderableColumns && this.d_columnOrder) {
                let orderedColumns = [];

                for (let columnKey of this.d_columnOrder) {
                    let column = this.findColumnByKey(cols, columnKey);

                    if (column && !this.columnProp(column, 'hidden')) {
                        orderedColumns.push(column);
                    }
                }

                return [...orderedColumns, ...cols.filter((item) => orderedColumns.indexOf(item) < 0)];
            }

            return cols;
        },
        columnGroups() {
            return this.d_columnGroups.get(this);
        },
        headerColumnGroup() {
            return this.columnGroups?.find((group) => this.columnProp(group, 'type') === 'header');
        },
        footerColumnGroup() {
            return this.columnGroups?.find((group) => this.columnProp(group, 'type') === 'footer');
        },
        hasFilters() {
            return this.filters && Object.keys(this.filters).length > 0 && this.filters.constructor === Object;
        },
        processedData() {
            let data = this.value || [];

            if (!this.lazy && !this.virtualScrollerOptions?.lazy) {
                if (data && data.length) {
                    if (this.hasFilters) {
                        data = this.filter(data);
                    }

                    if (this.sorted) {
                        if (this.sortMode === 'single') data = this.sortSingle(data);
                        else if (this.sortMode === 'multiple') data = this.sortMultiple(data);
                    }
                }
            }

            return data;
        },
        totalRecordsLength() {
            if (this.lazy) {
                return this.totalRecords;
            } else {
                const data = this.processedData;

                return data ? data.length : 0;
            }
        },
        empty() {
            const data = this.processedData;

            return !data || data.length === 0;
        },
        paginatorTop() {
            return this.paginator && (this.paginatorPosition !== 'bottom' || this.paginatorPosition === 'both');
        },
        paginatorBottom() {
            return this.paginator && (this.paginatorPosition !== 'top' || this.paginatorPosition === 'both');
        },
        sorted() {
            return this.d_sortField || (this.d_multiSortMeta && this.d_multiSortMeta.length > 0);
        },
        allRowsSelected() {
            if (this.selectAll !== null) {
                return this.selectAll;
            } else {
                const val = this.frozenValue ? [...this.frozenValue, ...this.processedData] : this.processedData;

                return isNotEmpty(val) && this.selection && Array.isArray(this.selection) && val.every((v) => this.selection.some((s) => this.equals(s, v)));
            }
        },
        groupRowSortField() {
            return this.sortMode === 'single' ? this.sortField : this.d_groupRowsSortMeta ? this.d_groupRowsSortMeta.field : null;
        },
        headerFilterButtonProps() {
            return {
                filter: { severity: 'secondary', text: true, rounded: true },
                ...this.filterButtonProps,
                inline: {
                    clear: { severity: 'secondary', text: true, rounded: true },
                    ...this.filterButtonProps.inline
                },
                popover: {
                    addRule: { severity: 'info', text: true, size: 'small' },
                    removeRule: { severity: 'danger', text: true, size: 'small' },
                    apply: { size: 'small' },
                    clear: { outlined: true, size: 'small' },
                    ...this.filterButtonProps.popover
                }
            };
        },
        rowEditButtonProps() {
            return {
                ...{
                    init: { severity: 'secondary', text: true, rounded: true },
                    save: { severity: 'secondary', text: true, rounded: true },
                    cancel: { severity: 'secondary', text: true, rounded: true }
                },
                ...this.editButtonProps
            };
        },
        virtualScrollerDisabled() {
            return isEmpty(this.virtualScrollerOptions) || !this.scrollable;
        }
    },
    components: {
        DTPaginator: Paginator,
        DTTableHeader: TableHeader,
        DTTableBody: TableBody,
        DTTableFooter: TableFooter,
        DTVirtualScroller: VirtualScroller,
        ArrowDownIcon: ArrowDownIcon,
        ArrowUpIcon: ArrowUpIcon,
        SpinnerIcon: SpinnerIcon
    }
};
</script>
```

```

primevue.org
PrimeVue | Vue UI Component Library
12–16 minutes
DataTable

DataTable displays data in tabular format.
Import #
Basic #

DataTable requires a value as data to display and Column components as children for the representation.

Code


Name


Category


Quantity
f230fh0g3	Bamboo Watch	Accessories	24
nvklal433	Black Watch	Accessories	61
zz21cz3c1	Blue Band	Fitness	2
244wgerg2	Blue T-Shirt	Clothing	25
h456wer53	Bracelet	Accessories	73
Dynamic Columns #

Columns can be created programmatically.

Code


Name


Category


Quantity
f230fh0g3	Bamboo Watch	Accessories	24
nvklal433	Black Watch	Accessories	61
zz21cz3c1	Blue Band	Fitness	2
244wgerg2	Blue T-Shirt	Clothing	25
h456wer53	Bracelet	Accessories	73
Template #

Custom content at header and footer sections are supported via templating.
Size #

In addition to a regular table, alternatives with alternative sizes are available.
Grid Lines #

Enabling showGridlines displays borders between cells.
Striped Rows #

Alternating rows are displayed when stripedRows property is present.
Basic #

Pagination is enabled by adding paginator property and defining rows per page.
Template #

Paginator UI is customized using the paginatorTemplate property. Each element can also be customized further with your own UI to replace the default one, refer to the Paginator component for more information about the advanced customization options.
Headless #

Headless mode on Pagination is enabled by adding using paginatorcontainer.
Sort #
Single Column #

Sorting on a column is enabled by adding the sortable property.
Multiple Columns #

Multiple columns can be sorted by defining sortMode as multiple. This mode requires metaKey (e.g. ⌘) to be pressed when clicking a header.
Presort #

Defining a default sortField and sortOrder displays data as sorted initially in single column sorting. In multiple sort mode, multiSortMeta should be used instead by providing an array of DataTableSortMeta objects.
Removable #

When removableSort is present, the third click removes the sorting from the column.
Filter #
Basic #

Data filtering is enabled by defining the filters model referring to a DataTableFilterMeta instance and specifying a filter element for a column using the filter template. This template receives a filterModel and filterCallback to build your own UI.

The optional global filtering searches the data against a single value that is bound to the global key of the filters object. The fields to search against are defined with the globalFilterFields.
Advanced #

When filterDisplay is set as menu, filtering UI is placed inside a popover with support for multiple constraints and advanced templating.
Row Selection #
Single #

Single row selection is enabled by defining selectionMode as single along with a value binding using selection property. When available, it is suggested to provide a unique identifier of a row with dataKey to optimize performance.

By default, metaKey press (e.g. ⌘) is necessary to unselect a row however this can be configured with disabling the metaKeySelection property. In touch enabled devices this option has no effect and behavior is same as setting it to false.
Multiple #

More than one row is selectable by setting selectionMode to multiple. By default in multiple selection mode, metaKey press (e.g. ⌘) is not necessary to add to existing selections. When the optional metaKeySelection is present, behavior is changed in a way that selecting a new row requires meta key to be present. Note that in touch enabled devices, DataTable always ignores metaKey.
RadioButton #

Specifying selectionMode as single on a Column, displays a radio button inside that column for selection. By default, row clicks also trigger selection, set selectionMode of DataTable to radiobutton to only trigger selection using the radio buttons.
Checkbox #

Specifying selectionMode as multiple on a Column, displays a checkbox inside that column for selection.

The header checkbox toggles the selection state of the whole dataset by default, when paginator is enabled you may add selectAll property and select-all-change event to only control the selection of visible rows.
Column #

Row selection with an element inside a column is implemented with templating.
Events #

DataTable provides row-select and row-unselect events to listen selection events.
Row Expansion #

Row expansion is controlled with expandedRows property. The column that has the expander element requires expander property to be enabled. Optional rowExpand and rowCollapse events are available as callbacks.

Expanded rows can either be an array of row data or when dataKey is present, an object whose keys are strings referring to the identifier of the row data and values are booleans to represent the expansion state e.g. {'1004': true}. The dataKey alternative is more performant for large amounts of data.
Edit #
Cell #

Cell editing is enabled by setting editMode as cell, defining input elements with editor templating of a Column and implementing cell-edit-complete to update the state.
Row #

Row editing is configured with setting editMode as row and defining editingRows with the v-model directive to hold the reference of the editing rows. Similarly with cell edit mode, defining input elements with editor slot of a Column and implementing row-edit-save are necessary to update the state. The column to control the editing state should have editor templating applied.
Vertical #

Adding scrollable property along with a scrollHeight for the data viewport enables vertical scrolling with fixed headers.
Flexible #

Flex scroll feature makes the scrollable viewport section dynamic instead of a fixed value so that it can grow or shrink relative to the parent size of the table. Click the button below to display a maximizable Dialog where data viewport adjusts itself according to the size changes.
Horizontal #

Horizontal scrollbar is displayed when table width exceeds the parent width.
Frozen Rows #

Rows can be fixed during scrolling by enabling the frozenValue property.
Frozen Columns #

A column can be fixed during horizontal scrolling by enabling the frozen property. The location is defined with the alignFrozen that can be left or right.
Preload #

Virtual Scrolling is an efficient way to render large amount data. Usage is similar to regular scrolling with the addition of virtualScrollerOptions property to define a fixed itemSize. Internally, VirtualScroller component is utilized so refer to the API of VirtualScroller for more information about the available options.

In this example, 100000 preloaded records are rendered by the Table.
Lazy #

When lazy loading is enabled via the virtualScrollerOptions, data is fetched on demand during scrolling instead of preload.

In sample below, an in-memory list and timeout is used to mimic fetching from a remote datasource. The virtualCars is an empty array that is populated on scroll.
Column Group #

Columns can be grouped within a Row component and groups can be displayed within a ColumnGroup component. These groups can be displayed using type property that can be header or footer. Number of cells and rows to span are defined with the colspan and rowspan properties of a Column.
Row Group #
Subheader #

Rows are grouped with the groupRowsBy property. When rowGroupMode is set as subheader, a header and footer can be displayed for each group. The content of a group header is provided with groupheader and footer with groupfooter slots.
Expandable #

When expandableRowGroups is present in subheader based row grouping, groups can be expanded and collapsed. State of the expansions are controlled using the expandedRows property and rowgroup-expand and rowgroup-collapse events.
RowSpan #

When rowGroupMode is configured to be rowspan, the grouping column spans multiple rows.
Conditional Style #

Particular rows and cells can be styled based on conditions. The rowClass receives a row data as a parameter to return a style class for a row whereas cells are customized using the body template.
Column Resize #
Fit Mode #

Columns can be resized with drag and drop when resizableColumns is enabled. Default resize mode is fit that does not change the overall table width.
Expand Mode #

Setting columnResizeMode as expand changes the table width as well.
Reorder #

Order of the columns and rows can be changed using drag and drop. Column reordering is configured by adding reorderableColumns property.

Similarly, adding rowReorder property to a column enables draggable rows. For the drag handle a column needs to have rowReorder property and table needs to have row-reorder event is required to control the state of the rows after reorder completes.
Column Toggle #

Column visibility based on a condition can be implemented with dynamic columns, in this sample a MultiSelect is used to manage the visible columns.
Export #

DataTable can export its data to CSV format.
Context Menu #

DataTable has exclusive integration with ContextMenu using the contextMenu event to open a menu on right click alont with contextMenuSelection property and row-contextmenu event to control the selection via the menu.
Stateful #

Stateful table allows keeping the state such as page, sort and filtering either at local storage or session storage so that when the page is visited again, table would render the data using the last settings.

Change the state of the table e.g paginate, navigate away and then return to this table again to test this feature, the setting is set as session with the stateStorage property so that Table retains the state until the browser is closed. Other alternative is local referring to localStorage for an extended lifetime.
Samples #
Customers #

DataTable with selection, pagination, filtering, sorting and templating.
Products #

CRUD implementation example with a Dialog.
Accessibility #
Screen Reader

DataTable uses a table element whose attributes can be extended with the tableProps option. This property allows passing aria roles and attributes like aria-label and aria-describedby to define the table for readers. Default role of the table is table. Header, body and footer elements use rowgroup, rows use row role, header cells have columnheader and body cells use cell roles. Sortable headers utilizer aria-sort attribute either set to "ascending" or "descending".

Built-in checkbox and radiobutton components for row selection use checkbox and radiobutton. The label to describe them is retrieved from the aria.selectRow and aria.unselectRow properties of the locale API. Similarly header checkbox uses selectAll and unselectAll keys. When a row is selected, aria-selected is set to true on a row.

The element to expand or collapse a row is a button with aria-expanded and aria-controls properties. Value to describe the buttons is derived from aria.expandRow and aria.collapseRow properties of the locale API.

The filter menu button use aria.showFilterMenu and aria.hideFilterMenu properties as aria-label in addition to the aria-haspopup, aria-expanded and aria-controls to define the relation between the button and the overlay. Popop menu has dialog role with aria-modal as focus is kept within the overlay. The operator dropdown use aria.filterOperator and filter constraints dropdown use aria.filterConstraint properties. Buttons to add rules on the other hand utilize aria.addRule and aria.removeRule properties. The footer buttons similarly use aria.clear and aria.apply properties. filterInputProps of the Column component can be used to define aria labels for the built-in filter components, if a custom component is used with templating you also may define your own aria labels as well.

Editable cells use custom templating so you need to manage aria roles and attributes manually if required. The row editor controls are button elements with aria.editRow, aria.cancelEdit and aria.saveEdit used for the aria-label.

Paginator is a standalone component used inside the DataTable, refer to the paginator for more information about the accessibility features.
Keyboard Support

Any button element inside the DataTable used for cases like filter, row expansion, edit are tabbable and can be used with space and enter keys.
Sortable Headers Keyboard Support
Key	Function
tab	Moves through the headers.
enter	Sorts the column.
space	Sorts the column.
Filter Menu Keyboard Support
Key	Function
tab	Moves through the elements inside the popup.
escape	Hides the popup.
Selection Keyboard Support
Key	Function
tab	Moves focus to the first selected row, if there is none then first row receives the focus.
up arrow	Moves focus to the previous row.
down arrow	Moves focus to the next row.
enter	Toggles the selected state of the focused row depending on the metaKeySelection setting.
space	Toggles the selected state of the focused row depending on the metaKeySelection setting.
home	Moves focus to the first row.
end	Moves focus to the last row.
shift + down arrow	Moves focus to the next row and toggles the selection state.
shift + up arrow	Moves focus to the previous row and toggles the selection state.
shift + space	Selects the rows between the most recently selected row and the focused row.
control + shift + home	Selects the focused rows and all the options up to the first one.
control + shift + end	Selects the focused rows and all the options down to the last one.
control + a	Selects all rows.
```

Go through these and implement changes/performance refactors.

```
<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick, watch, computed, reactive } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DateRangeFilter from '@/components/DateRangeFilter.vue'
import { api } from '@/services/api'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Tag from 'primevue/tag'
import Skeleton from 'primevue/skeleton'
import { useToast } from 'primevue/usetoast'
import Toast from 'primevue/toast'
import Drawer from 'primevue/drawer'
import Paginator from 'primevue/paginator'
import type { Log, LogResponse } from '@/types/logs'
import type { Source } from '@/types/source'
import Select from 'primevue/select'
import FloatLabel from 'primevue/floatlabel'
import Button from 'primevue/button'
import LogSchemaSidebar from '@/components/LogSchemaSidebar.vue'
import LogFieldValue from '@/components/LogFieldValue.vue'
import MultiSelect from 'primevue/multiselect'
import { useDebounceFn } from '@vueuse/core'

// Move state declarations to the top
const logs = ref<Log[]>([])
const loading = ref(false)
const sourcesLoading = ref(true)
const error = ref<string | null>(null)
const route = useRoute()
const router = useRouter()
const sources = ref<Source[]>([])
const rowsPerPageOptions = [50, 100, 500, 1000]
const limit = ref(50)
const hasMore = ref(true)
const totalRecords = ref(0)
const lazyState = ref({
  first: 0,
  rows: limit.value
})
const schema = ref<any[]>([])

// Props and other refs
const props = defineProps<{
  sourceId?: string
  initialStartTime?: string
  initialEndTime?: string
  initialSearchQuery?: string
  initialSeverity?: string
}>()

const sourceId = ref<string | undefined>(props.sourceId)
const startDate = ref(
  props.initialStartTime
    ? new Date(props.initialStartTime)
    : new Date(Date.now() - 60 * 60 * 1000)
)
const endDate = ref(
  props.initialEndTime
    ? new Date(props.initialEndTime)
    : new Date()
)

// Add loading state refs
const tableLoading = ref(false)
const initialLoad = ref(true) // Track first load to show skeleton

// Add new ref for query mode
const queryMode = ref<'basic' | 'logchefql' | 'sql'>('basic')
const queryString = ref('')

// Now define loadLogs and resetLogs
const loadLogs = async () => {
  if (!sourceId.value) {
    error.value = 'No source selected'
    return
  }

  tableState.loading = true
  error.value = null

  try {
    const response = await api.getLogs(sourceId.value, {
      start_time: startDate.value?.toISOString(),
      end_time: endDate.value?.toISOString(),
      offset: lazyState.value.first,
      limit: lazyState.value.rows,
      search_query: searchQuery.value,
      severity_text: severityText.value
    })

    logs.value = response.logs || []
    tableState.totalRecords = response.total_count || 0
    hasMore.value = response.has_more || false
  } catch (err) {
    console.error('Error fetching logs:', err)
    error.value = err instanceof Error ? err.message : 'Failed to fetch logs'
    logs.value = []
  } finally {
    tableState.loading = false
    initialLoad.value = false
  }
}

const resetLogs = async () => {
  lazyState.value = {
    first: 0,
    rows: lazyState.value.rows
  }
  await loadLogs()
}

// Create computed property for URL parameters
const queryParams = computed(() => ({
  source: sourceId.value,
  start_time: startDate.value?.toISOString(),
  end_time: endDate.value?.toISOString()
}))

// Update URL when parameters change
watch([sourceId, startDate, endDate], () => {
  router.replace({
    query: {
      ...route.query,
      ...queryParams.value
    }
  })
}, { deep: true })

const offset = ref(0)
const toast = useToast()

// Type for the selected log
const selectedLog = ref<Log | null>(null)
const drawerVisible = ref(false)

const showLogDetails = (event: { data: Log }) => {
  selectedLog.value = event.data
  drawerVisible.value = true
}

const copyToClipboard = async (data: Log | null) => {
  if (!data) return
  try {
    await navigator.clipboard.writeText(JSON.stringify(data, null, 2))
    toast.add({
      severity: 'success',
      summary: 'Copied',
      detail: 'Log data copied to clipboard',
      life: 2000
    })
  } catch (err) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to copy to clipboard',
      life: 3000
    })
  }
}

const isProgressiveLoading = ref(false)
const progressPercentage = ref(0)

// Initial fetch of sources to get the first sourceId
async function fetchSources() {
  try {
    sourcesLoading.value = true
    const sourcesData = await api.fetchSources()
    if (sourcesData && sourcesData.length > 0) {
      sources.value = sourcesData
      sourceId.value = sourcesData[0].ID
    } else {
      error.value = 'No sources available'
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to fetch sources'
  } finally {
    sourcesLoading.value = false
  }
}

const getSeverityType = (severityText) => {
  switch (severityText?.toLowerCase()) {
    case 'error': return 'danger'
    case 'warn': return 'warning'
    case 'info': return 'secondary'
    case 'debug': return 'success'
    default: return null
  }
}

// Function to fetch both logs and schema concurrently
const fetchLogsAndSchema = async () => {
  if (!sourceId.value) return

  try {
    loading.value = true
    tableLoading.value = true
    error.value = null

    const controller = new AbortController()
    const signal = controller.signal

    const [logsResponse, schemaResponse] = await Promise.all([
      api.getLogs(sourceId.value, {
        start_time: startDate.value?.toISOString(),
        end_time: endDate.value?.toISOString(),
        offset: lazyState.value.first,
        limit: lazyState.value.rows,
        search_query: '',
        severity_text: ''
      }, signal),
      api.getLogSchema(sourceId.value, {
        start_time: startDate.value?.toISOString(),
        end_time: endDate.value?.toISOString()
      }, signal)
    ])

    if (!signal.aborted) {
      logs.value = logsResponse.logs || []
      totalRecords.value = logsResponse.total_count || 0
      hasMore.value = logsResponse.has_more || false
      schema.value = schemaResponse || []
    }
  } catch (err) {
    if (err.name !== 'AbortError') {
      console.error('Error fetching data:', err)
      error.value = err instanceof Error ? err.message : 'Failed to fetch data'
      logs.value = []
      schema.value = []
    }
  } finally {
    loading.value = false
    tableLoading.value = false
    initialLoad.value = false
  }
}

// Initialize data on mount
onMounted(async () => {
  await fetchSources()

  if (!sourceId.value && sources.value.length > 0) {
    sourceId.value = sources.value[0].ID
  }

  if (sourceId.value) {
    startDate.value = props.initialStartTime
      ? new Date(props.initialStartTime)
      : new Date(Date.now() - 60 * 60 * 1000)
    endDate.value = props.initialEndTime
      ? new Date(props.initialEndTime)
      : new Date()

    await fetchLogsAndSchema()
  }
})

// Add a method to get shareable URL
const getShareableURL = () => {
  const url = new URL(window.location.href)
  url.search = new URLSearchParams(queryParams.value as Record<string, string>).toString()
  return url.toString()
}

// Add copy URL button
const copyShareableURL = async () => {
  try {
    await navigator.clipboard.writeText(getShareableURL())
    toast.add({
      severity: 'success',
      summary: 'URL Copied',
      detail: 'Shareable URL has been copied to clipboard',
      life: 2000
    })
  } catch (err) {
    toast.add({
      severity: 'error',
      summary: 'Error',
      detail: 'Failed to copy URL',
      life: 3000
    })
  }
}

const columns = computed(() => {
  const result = []

  // Add default columns first
  result.push(
    { field: 'timestamp', header: 'Timestamp', required: true },
    { field: 'severity_text', header: 'Severity', required: false },
    { field: 'body', header: 'Message', required: false }
  )

  // Process schema fields
  schema.value?.forEach(field => {
    if (field.parent === 'log_attributes') {
      // For nested fields under log_attributes
      result.push({
        field: `log_attributes.${field.path[field.path.length - 1]}`,
        header: field.path[field.path.length - 1],
        required: false,
        type: field.type
      })
    } else if (field.children) {
      // For fields with children (nested structures)
      field.children.forEach(child => {
        result.push({
          field: child.path.join('.'),
          header: child.path[child.path.length - 1],
          required: false,
          type: child.type
        })
      })
    } else if (!['timestamp', 'severity_text', 'body'].includes(field.path[0])) {
      // For regular fields, excluding the ones we already added
      result.push({
        field: field.path.join('.'),
        header: field.path[0],
        required: false,
        type: field.type
      })
    }
  })

  return result
})

const selectedColumns = ref([
  { field: 'timestamp', header: 'Timestamp', required: true },
  { field: 'severity_text', header: 'Severity', required: false },
  { field: 'body', header: 'Message', required: false }
])

const onToggleColumns = (val) => {
  // Always include required columns (like timestamp)
  const requiredColumns = columns.value.filter(col => col.required)
  selectedColumns.value = [
    ...requiredColumns,
    ...val.filter(col => !col.required)
  ]
}

// Replace updateVisibleColumns with this
const updateVisibleColumns = (fields: string[]) => {
  selectedColumns.value = columns.value.filter(col =>
    col.required || fields.includes(col.field)
  )
}

// Update sortedVisibleColumns computed property
const sortedVisibleColumns = computed(() =>
  selectedColumns.value.map(col => col.field)
)

const getNestedValue = (obj: any, path: string) => {
    if (!obj) return undefined

    // Special handling for nested attributes
    if (path.startsWith('log_attributes.')) {
        const [_, ...attributePath] = path.split('.')
        const value = obj.log_attributes?.[attributePath.join('.')]
        return value === undefined ? '-' : value
    }

    // For regular fields
    const value = path.split('.').reduce((acc, part) => {
        if (acc === undefined) return undefined
        return acc[part]
    }, obj)

    return value === undefined ? '-' : value
}

const formatColumnHeader = (field: string) => {
    // Remove duplicate log_attributes prefix if present
    return field.replace('log_attributes.log_attributes.', 'log_attributes.')
}

// Add function to handle column styles
const getColumnStyle = (field: string) => {
    const isNested = field.includes('.')

    switch (field) {
        case 'timestamp':
            return {
                width: '160px',
                minWidth: '160px',
                backgroundColor: 'white'
            }
        case 'severity_text':
            return {
                width: '90px',
                minWidth: '90px',
                backgroundColor: 'white'
            }
        case 'body':
            return {
                minWidth: '300px',
                backgroundColor: 'white'
            }
        default:
            return {
                ...(isNested
                    ? { width: '140px', minWidth: '140px' }
                    : { minWidth: '120px' }
                ),
                backgroundColor: 'white'
            }
    }
}

// Format timestamp for developer-friendly display
const formatTimestamp = (timestamp: string) => {
  try {
    const date = new Date(timestamp)
    if (isNaN(date.getTime())) {
      throw new Error('Invalid date')
    }

    // Format: Nov 26 2024 11:30:34.000 UTC
    const formatter = new Intl.DateTimeFormat('en-US', {
      month: 'short',
      day: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3,
      timeZone: 'UTC',
      hour12: false
    })

    return formatter.format(date) + ' UTC'
  } catch (err) {
    console.error('Error formatting timestamp:', timestamp, err)
    return timestamp
  }
}

// Update pagination methods
const prevPage = () => {
  if (lazyState.value.first > 0) {
    lazyState.value.first -= lazyState.value.rows
    loadLogs()
    const tableContainer = document.querySelector('.flex-1.overflow-y-auto')
    tableContainer?.scrollTo({ top: 0, behavior: 'smooth' })
  }
}

const nextPage = () => {
  if (hasMore.value) {
    lazyState.value.first += lazyState.value.rows
    loadLogs()
    const tableContainer = document.querySelector('.flex-1.overflow-y-auto')
    tableContainer?.scrollTo({ top: 0, behavior: 'smooth' })
  }
}

// Add a watcher for lazyState.rows changes
watch(() => lazyState.value.rows, (newLimit) => {
  // Reset to first page when limit changes
  lazyState.value.first = 0
  loadLogs()
}, { immediate: false })

const dt = ref()

// Add export function that handles dynamic columns
const exportLogs = () => {
  if (!dt.value || !logs.value.length) return

  // Get all selected columns
  const columnsToExport = selectedColumns.value.map(col => ({
    field: col.field,
    header: formatColumnHeader(col.field)
  }))

  // Prepare CSV data
  const csvData = logs.value.map(log => {
    const row = {}
    columnsToExport.forEach(col => {
      row[col.header] = getNestedValue(log, col.field)
    })
    return row
  })

  // Convert to CSV
  const headers = columnsToExport.map(col => col.header)
  const csvContent = [
    headers.join(','),
    ...csvData.map(row => headers.map(header => {
      const value = row[header]
      // Handle values that might contain commas or quotes
      const cellValue = String(value).replace(/"/g, '""')
      return `"${cellValue}"`
    }).join(','))
  ].join('\n')

  // Create and trigger download
  const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.setAttribute('download', `logs_export_${new Date().toISOString()}.csv`)
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

// Add new state management
const tableState = reactive({
  rows: 50,
  totalRecords: 0,
  loading: false,
  virtualLoading: false
})

// Add optimized lazy loading handler
const onLazyLoad = async (event) => {
  if (tableState.loading) return

  tableState.loading = true
  try {
    const response = await api.getLogs(sourceId.value, {
      start_time: startDate.value?.toISOString(),
      end_time: endDate.value?.toISOString(),
      offset: event.first,
      limit: event.rows,
      search_query: searchQuery.value,
      severity_text: severityText.value
    })

    logs.value = response.logs
    tableState.totalRecords = response.total_count
    hasMore.value = response.has_more
  } finally {
    tableState.loading = false
  }
}

// Add debounced scroll handler
const debouncedScroll = useDebounceFn(() => {
  if (tableState.virtualLoading) return
  // Scroll handling logic here
}, 150)
</script>

<template>
  <div class="h-screen flex flex-col overflow-hidden">
    <!-- Main Content Area -->
    <div class="flex-1 flex flex-col">
      <!-- Fixed Header -->
      <div class="flex-none bg-white border-b border-gray-200">
        <!-- Primary Controls -->
        <div class="p-4 border-b border-gray-100">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-4 flex-1">
              <!-- Left side: Source and Columns -->
              <div class="flex items-center gap-3 flex-1">
                <FloatLabel class="w-[250px]" variant="on">
                  <Select
                    v-model="sourceId"
                    :options="sources"
                    optionLabel="Name"
                    optionValue="ID"
                    class="w-full"
                    @change="resetLogs"
                  />
                  <label>Choose Log Source</label>
                </FloatLabel>

                <FloatLabel class="w-[350px]" variant="on">
                  <MultiSelect
                    v-model="selectedColumns"
                    :options="columns"
                    optionLabel="header"
                    display="chip"
                    class="w-full"
                    @update:modelValue="onToggleColumns"
                    :disabled="tableLoading"
                  />
                  <label>Select Columns</label>
                </FloatLabel>
              </div>

              <!-- Right side: Time Range -->
              <div class="flex items-center gap-3">
                <FloatLabel class="min-w-[400px]" variant="on">
                  <DateRangeFilter
                    v-model:startDate="startDate"
                    v-model:endDate="endDate"
                    @fetch="fetchLogsAndSchema"
                    class="w-full"
                  />
                  <label>Time Range</label>
                </FloatLabel>
                <div class="h-6 w-px bg-gray-200 mx-2"></div>
                <Button
                  icon="pi pi-share-alt"
                  severity="secondary"
                  text
                  v-tooltip.bottom="'Share Query'"
                  @click="copyShareableURL"
                />
              </div>
            </div>
          </div>
        </div>

        <!-- Secondary Controls: Pagination and Actions -->
        <div class="px-4 py-2 bg-gray-50/50 flex items-center justify-between">
          <div class="flex items-center gap-3">
            <span class="text-sm text-gray-600">
              <span v-if="tableLoading">Loading logs...</span>
              <span v-else>{{ totalRecords }} logs found</span>
            </span>
            <div class="h-4 w-px bg-gray-200"></div>
            <Button
              icon="pi pi-download"
              severity="secondary"
              text
              @click="exportLogs"
              v-tooltip.bottom="'Export as CSV'"
              :disabled="!logs.length"
            />
          </div>

          <!-- Pagination Controls -->
          <div class="flex items-center gap-3">
            <span class="text-sm text-gray-600">Show:</span>
            <Select
              v-model="lazyState.rows"
              :options="rowsPerPageOptions"
              class="w-[100px]"
              @change="resetLogs"
            />
            <div class="flex items-center gap-1">
              <Button
                icon="pi pi-angle-left"
                text
                :disabled="lazyState.first === 0"
                @click="prevPage"
              />
              <span class="text-sm text-gray-600 min-w-[80px] text-center">
                {{ Math.floor(lazyState.first / lazyState.rows) + 1 }} of {{ Math.ceil(totalRecords / lazyState.rows) }}
              </span>
              <Button
                icon="pi pi-angle-right"
                text
                :disabled="!hasMore"
                @click="nextPage"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Rest of the DataTable code remains the same -->
      <div class="flex-1 overflow-y-auto">
        <!-- Loading skeleton for initial load -->
        <div v-if="initialLoad" class="p-4 space-y-4">
          <div v-for="i in 5" :key="i" class="animate-pulse space-y-2">
            <div class="h-10 bg-gray-100 rounded-md"></div>
          </div>
        </div>

        <!-- Error state -->
        <div v-else-if="error" class="p-8 text-center">
          <div class="text-red-500 mb-2">{{ error }}</div>
          <Button label="Retry" severity="secondary" @click="loadLogs" />
        </div>

        <!-- Empty state -->
        <div v-else-if="!logs.length && !tableState.loading" class="p-8 text-center">
          <div class="text-gray-500 mb-2">No logs found for the selected time range</div>
          <Button label="Change Filters" severity="secondary" @click="() => {}" />
        </div>

        <!-- DataTable -->
        <DataTable
          v-else
          ref="dt"
          :value="logs"
          :loading="tableState.loading"
          dataKey="timestamp"
          :virtualScrollerOptions="{
            itemSize: 46,
            delay: 150,
            showLoader: true
          }"
          :virtualScroller="true"
          class="logs-table"
          selectionMode="single"
          @row-click="showLogDetails"
          :resizableColumns="true"
          columnResizeMode="fit"
          scrollable
          scrollHeight="calc(100vh - 200px)"
          :rows="tableState.rows"
          :totalRecords="tableState.totalRecords"
          @page="onLazyLoad"
          v-bind:pt="{
            root: { class: 'h-full' },
            wrapper: { class: 'h-full' },
            table: { class: 'text-sm border-t border-gray-200' },
            bodyCell: {
              class: ['p-1.5 border-b border-gray-100'],
              style: { contain: 'strict' }
            },
            headerCell: {
              class: [
                'p-2 bg-white border-b border-gray-200 font-medium text-gray-700',
                'sticky top-0 z-20'
              ]
            },
            bodyRow: {
              class: ['hover:bg-blue-50/50 cursor-pointer transition-colors duration-100'],
              style: { contain: 'content' }
            }
          }"
        >
          <!-- Column definitions -->
          <Column v-for="field in sortedVisibleColumns"
                  :key="field"
                  :field="field"
                  :header="formatColumnHeader(field)"
                  :style="getColumnStyle(field)"
          >
            <template #body="{ data }">
              <LogFieldValue
                  :field="field"
                  :value="getNestedValue(data, field)"
              />
            </template>
          </Column>
        </DataTable>
      </div>
    </div>

    <!-- Simplified Log Details Drawer -->
    <Drawer
      v-model:visible="drawerVisible"
      position="right"
      :modal="true"
      :dismissable="true"
      :closable="true"
      :style="{ width: 'min(85vw, 960px)' }"
      class="drawer-wide"
      :pt="{
        root: { class: 'border-l border-gray-200', style: { width: 'min(85vw, 960px)' } },
        header: { class: 'bg-gray-50 px-6 py-4 border-b border-gray-200' },
        content: { class: 'p-6 overflow-y-auto' }
      }"
    >
      <template #header>
        <div class="flex justify-between items-center">
          <h3 class="text-lg font-semibold text-gray-900">Log Details</h3>
          <button
            @click="copyToClipboard(selectedLog)"
            class="text-gray-400 hover:text-gray-600 p-2 rounded-md hover:bg-gray-100"
            title="Copy to clipboard"
          >
            <i class="pi pi-copy"></i>
          </button>
        </div>
      </template>

      <pre v-if="selectedLog" class="text-sm font-mono bg-gray-50 p-4 rounded-md overflow-x-auto whitespace-pre-wrap">{{ JSON.stringify(selectedLog, null, 2) }}</pre>
    </Drawer>
  </div>
</template>

<style>
/* Update scrollHeight calculation in styles */
.logs-table {
  height: 100%;
}

/* Clean up styles */
.logs-table {
  height: 100%;
}

/* Remove all wrapper scrolling */
.logs-table .p-datatable-wrapper {
  border: none;
}

/* Ensure header is properly positioned */
.logs-table .p-datatable-thead > tr > th {
  background-color: white !important;
  position: sticky !important;
  top: 0 !important;
  z-index: 2 !important;
}

/* Scrollable table styles */
.logs-table .p-datatable-wrapper {
  height: 100%;
  border-radius: 6px;
}

/* Loading state */
.p-datatable-loading-overlay {
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(2px);
}

/* Skeleton animation */
@keyframes pulse {
  0%, 100% { opacity: 0.5; }
  50% { opacity: 1; }
}

.animate-pulse {
  animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}

/* Drawer styles */
:deep(.p-drawer) {
  width: min(85vw, 960px) !important;
  max-width: none !important;
}

:deep(.p-drawer-content) {
  width: 100% !important;
}

/* Add these optimized styles */
.logs-table {
  height: 100%;
  contain: strict;  /* Add CSS containment */
}

.logs-table .p-virtualscroller {
  scroll-behavior: smooth;
  will-change: transform;  /* Optimize scrolling */
}

/* Optimize fixed header */
.logs-table .p-datatable-thead > tr > th {
  position: sticky;
  top: 0;
  z-index: 2;
  background: white;
  contain: style layout;  /* Add containment */
}

/* Add will-change for better performance */
.logs-table .p-datatable-tbody > tr {
  will-change: transform;
  contain: content;  /* Add containment */
}

/* Optimize scrolling */
.logs-table .p-datatable-wrapper {
  contain: strict;
  overflow-anchor: none;  /* Prevent scroll anchoring */
}

/* Add GPU acceleration for animations */
.logs-table .p-datatable-tbody > tr:hover {
  transform: translateZ(0);
}
</style>
```

First, reason out and think clearly of what all changes we can do. Then we can proceed.

I also want the application to load like 100-200 logs, and when user scrolls, then load more. Maintain ordering is critical here, as these are logs. The backend APIs support fetching logs with offset and pagination/limit, so you have to incorporate frontend to maintain the ordering.