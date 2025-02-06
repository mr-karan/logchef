Below is a sample Product Requirements Document (PRD) with runbook-style, task‐by‐task instructions to build a Cloudflare-inspired **Filters** component for your log analytics app using **shadcn/ui (Vue)**.

---

# Product Requirements Document (PRD)

## 1. Overview

**Title:** Log Analytics Filters Component
**Author:** [Your Name]
**Date:** [Current Date]
**Status:** Draft / In Review

### 1.1. Objective

Build an intuitive, visually clean Filters component—mirroring Cloudflare’s Analytics page—that allows users to construct filter queries for log analytics. This component will be integrated with your backend API, which accepts a structured JSON payload representing filter groups and conditions.

### 1.2. Background

Users of the log analytics app need to dynamically query logs using various conditions (e.g., by service name, severity, country, etc.). The backend API expects a JSON structure like the following:

```json
{
  "start_timestamp": 1737225000000,
  "end_timestamp": 1737484200000,
  "limit": 100,
  "mode": "filters",
  "filterGroups": [
    {
      "operator": "OR",
      "conditions": [
        {
          "field": "service_name",
          "operator": "=",
          "value": "AmbientTech"
        },
        {
          "field": "severity_text",
          "operator": "!=",
          "value": "ERROR"
        }
      ]
    }
  ]
}
```

Your new component will let users construct these filter objects with a friendly UI.

---

## 2. Scope

### 2.1. In Scope

- **Header Section:**
  - Title: “Web Analytics for all sites”
  - Time Range Dropdown (e.g., “Previous 30 days”, etc.)
  - “Print Report” button with an icon

- **Filter Interface:**
  - “Add Filter” button (with plus icon)
  - A card-based layout for each filter group or condition with:
    - **Field Selector:** Options like “Country”, “Service Name”, “severity_text”, etc.
    - **Operator Selector:** Options such as “equals”, “does not equal”, etc.
    - **Value Input/Selector:** Depending on field type (text input or dropdown)
    - “Cancel” and “Apply” buttons for each filter card

- **Styling:**
  - Light, clean, and consistent styling following Cloudflare’s design cues
  - Blue accent color for primary actions
  - Consistent spacing, alignment, and dropdown widths

- **Integration:**
  - Convert filter selections into the required API JSON structure
  - Support combining multiple conditions in a single filter group (using an “OR” operator as an initial design)

### 2.2. Out of Scope

- Saving filter presets (this can be a future enhancement)
- Drag-and-drop reordering of filters (consider as a stretch goal)

---

## 3. Functional Requirements

1. **Header Layout:**
   - Display a header with:
     - Title (“Web Analytics for all sites”)
     - Time range dropdown with common options (e.g., “Previous 30 days”)
     - A “Print Report” button accompanied by an icon

2. **Filter Management:**
   - Display an “Add Filter” button with a plus icon.
   - On clicking “Add Filter”, render a new filter card.

3. **Filter Card Elements:**
   - **Field Selector:**
     - Dropdown populated with fields such as “Country”, “Service Name”, “severity_text”, etc.
   - **Operator Selector:**
     - Dropdown with operators (e.g., “=”, “!=”, etc.)
   - **Value Input:**
     - A text input or another dropdown, depending on the field type.
   - **Action Buttons:**
     - “Cancel” to dismiss the card (or reset the values)
     - “Apply” to save the filter condition

4. **Filter Data Handling:**
   - Maintain state for active filter groups and conditions.
   - Convert the UI state into the backend API JSON structure on “Apply”.

5. **Error Handling & Validation:**
   - Validate that all fields in a filter card are completed before enabling “Apply”.
   - Provide inline feedback for missing or invalid values.

---

## 4. Non-Functional Requirements

- **Usability:**
  - Clean, intuitive interface following Cloudflare’s aesthetics.
  - Clear visual hierarchy and ample whitespace.

- **Performance:**
  - Fast UI interactions with minimal lag.

- **Accessibility:**
  - Keyboard navigable and screen-reader friendly components.

- **Consistency:**
  - Uniform styling for dropdowns, buttons, and cards in accordance with shadcn/ui guidelines.

- **Responsiveness:**
  - Works seamlessly on desktop and mobile (if applicable).

---

## 5. User Stories

1. **As a user,** I want to add filters easily so that I can narrow down log analytics data.
2. **As a user,** I want to quickly see my applied filters and adjust them as needed.
3. **As a user,** I want the filter component to be intuitive and visually clean, similar to Cloudflare’s design.

---

## 6. UI/UX Design Considerations

- **Visual Hierarchy:**
  - The header should be immediately visible.
  - The “Add Filter” button should be prominent.

- **Component Layout:**
  - Use a card-based layout for each filter entry.
  - Ensure alignment and spacing mimic Cloudflare’s light and minimalistic style.

- **Interactive Elements:**
  - Provide hover and active states for buttons.
  - Include clear feedback (e.g., loading indicators, validation errors).

---

## 7. Runbook: Step-by-Step Task List

### **Phase 1: Environment Setup & Planning**

1. **Setup Project Environment:**
   - Verify that the Vue project is set up with shadcn/ui installed.
   - Ensure proper tooling (e.g., ESLint, Prettier, unit testing libraries) is in place.

2. **Review API Specifications:**
   - Confirm the filter JSON structure expected by the backend.
   - Identify all available fields and operators supported by your API.

3. **Design Wireframes:**
   - Sketch the header layout, filter card, and overall component.
   - Review and finalize the wireframes with the design team.

---

### **Phase 2: Component Structure & UI Implementation**

4. **Create Base Component Structure:**
   - **Task:** Create a new Vue component (e.g., `FiltersComponent.vue`).
   - **Subtasks:**
     - Set up a basic template with a header section and a container for filter cards.
     - Implement the header with title, time range dropdown, and “Print Report” button.
   - **Deliverable:** Base component integrated into the log analytics page.

5. **Implement “Add Filter” Functionality:**
   - **Task:** Add an “Add Filter” button with a plus icon.
   - **Subtasks:**
     - On button click, append a new filter card component to the filter container.
   - **Deliverable:** Dynamic creation of filter cards on user interaction.

6. **Develop Filter Card Component:**
   - **Task:** Create a reusable `FilterCard.vue` component.
   - **Subtasks:**
     - Add a field selector dropdown (populate with fields like “Country”, “Service Name”, “severity_text”, etc.).
     - Add an operator selector dropdown (populate with operators such as “=”, “!=”, etc.).
     - Add a value input field (text input or dropdown as appropriate).
     - Add “Cancel” and “Apply” buttons.
   - **Deliverable:** A fully functional and styled filter card.

7. **State Management & Data Binding:**
   - **Task:** Implement state management (using Vue’s reactive state or Vuex/Pinia if already in use) to track active filters.
   - **Subtasks:**
     - Bind form inputs to the component state.
     - Manage addition, update, and removal of filter conditions.
   - **Deliverable:** A data model that mirrors the filter JSON structure.

---

### **Phase 3: API Integration & Business Logic**

8. **Implement API Integration:**
   - **Task:** Develop a function to convert the UI state into the expected API JSON.
   - **Subtasks:**
     - Map each filter card’s state to an object within `filterGroups` (start with a default “OR” group).
     - Ensure proper conversion of timestamps, limits, and other global parameters.
   - **Deliverable:** A utility function or service that prepares the API payload.

9. **Validation and Error Handling:**
   - **Task:** Add validation to ensure all filter cards have valid selections before applying.
   - **Subtasks:**
     - Disable the “Apply” button if required fields are empty.
     - Show inline error messages for invalid input.
   - **Deliverable:** Robust validation and user feedback mechanisms.

---

### **Phase 4: Styling, Testing, and Documentation**

10. **Apply Styling with shadcn/ui:**
    - **Task:** Use shadcn/ui styling to ensure the component matches Cloudflare’s design cues.
    - **Subtasks:**
      - Use consistent spacing, fonts, and blue accent colors for primary actions.
      - Ensure dropdowns, cards, and buttons have a uniform appearance.
    - **Deliverable:** A polished, Cloudflare-inspired UI.

11. **Testing:**
    - **Task:** Write unit and integration tests.
    - **Subtasks:**
      - Test UI interactions (e.g., adding a filter card, canceling, applying a filter).
      - Test the conversion function to ensure the API payload is correctly formatted.
    - **Deliverable:** A set of passing tests that cover major functionalities.

12. **Documentation:**
    - **Task:** Update internal documentation and user guides.
    - **Subtasks:**
      - Provide usage examples for the Filters component.
      - Document any configuration options or state management details.
    - **Deliverable:** Clear documentation for both developers and end users.

13. **Review & QA:**
    - **Task:** Conduct code reviews and UI/UX testing sessions.
    - **Subtasks:**
      - Gather feedback from stakeholders.
      - Make iterative adjustments based on QA findings.
    - **Deliverable:** A reviewed and QA-approved Filters component.

---

## 8. Timeline & Milestones

- **Week 1:** Environment setup, wireframing, and initial component scaffolding.
- **Week 2:** Implement header and filter card UI components.
- **Week 3:** State management, API integration, and validations.
- **Week 4:** Styling refinements, testing, and documentation.
- **Week 5:** Final review, QA, and deployment preparation.

---

## 9. Dependencies & Risks

- **Dependencies:**
  - shadcn/ui library
  - Vue and associated state management (Vuex/Pinia)
  - Backend API readiness and documentation
- **Risks:**
  - Misalignment between UI state and API payload structure
  - Delays in obtaining feedback from design or QA teams
  - Potential edge cases in filter logic (e.g., handling multiple filter groups)

---

## 10. Future Enhancements (Out of Scope for This Release)

- **Saved Filter Presets:** Allow users to save and load filter configurations.
- **Drag-and-Drop Reordering:** Enable users to reorder filters and groups.
- **Advanced Operators/Field Types:** Introduce more complex filtering logic based on additional operators and field types.

---

# Conclusion

This PRD outlines the requirements and detailed runbook steps to build a Cloudflare-inspired Filters component using shadcn/ui (Vue). By following the outlined tasks—from environment setup through final QA—the development team should be able to deliver a clean, functional, and intuitive filter interface that meets user needs and integrates seamlessly with your log analytics backend.

Feel free to adjust any specifics to better match your internal processes or further refine the component’s scope.