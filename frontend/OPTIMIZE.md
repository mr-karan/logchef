# Bundle Size Optimization Guide

This guide provides instructions for analyzing and optimizing the frontend bundle size for better performance.

## Analyzing Bundle Size

The project is configured with tools to analyze bundle size and identify optimization opportunities:

```bash
# Build with bundle analyzer visualization
pnpm build:analyze

# After building, run the custom analyzer script
pnpm analyze
```

The first command generates a visual representation of the bundle in `stats.html` and the second command provides recommendations for optimization.

## Optimization Strategies

### 1. Code Splitting

The Vue Router is configured to use code splitting via dynamic imports. All routes use lazy loading to only load components when needed. The router uses a custom `lazyLoad` helper function to handle errors consistently.

### 2. Modern Bundle

For modern browsers, build an optimized version with:

```bash
pnpm build:modern
```

This builds a version targeting modern browsers with ES modules, which results in smaller bundle sizes.

### 3. Tree Shaking

The build is configured to use tree shaking to eliminate dead code. To ensure effective tree shaking:

- Use ES modules syntax (import/export)
- Avoid side effects in modules
- Use named imports instead of namespace imports
- Configure `sideEffects: false` in package.json for your own modules

### 4. Dependency Optimization

Large dependencies can significantly impact bundle size:

- Use `lodash-es` instead of `lodash` for tree-shaking
- Import only needed components from UI libraries
- Consider replacing heavy libraries with lighter alternatives
- Check for duplicate dependencies

### 5. Image and Asset Optimization

- Use appropriate image formats (WebP for photos, SVG for icons)
- Compress images using appropriate tools
- Use responsive images with `srcset`

### 6. Lazy Loading Components

For large components not needed immediately:

```js
// Instead of
import HeavyComponent from "@/components/HeavyComponent.vue";

// Use
const HeavyComponent = () => import("@/components/HeavyComponent.vue");
```

### 7. Monaco Editor Optimization

Monaco Editor is particularly large. The current configuration separates it into its own chunk. Consider:

- Loading it only when needed
- Using a lighter alternative for simple code editing
- Further code-splitting Monaco Editor features

### 8. Performance Monitoring

Regularly analyze bundle size changes:

```bash
# Before committing changes:
pnpm build:analyze
pnpm analyze
```

## Common Issues

1. **Large Vendor Bundles**: Check for duplicate dependencies or unnecessary imports
2. **Duplicate Code**: Look for modules imported in multiple chunks
3. **Unused Dependencies**: Regularly audit and remove unused dependencies
4. **Large Components**: Break down large components into smaller ones

## Additional Resources

- [Vue Performance Guide](https://vuejs.org/guide/best-practices/performance.html)
- [Vite Build Optimization](https://vitejs.dev/guide/build.html)
- [Web.dev Performance Guide](https://web.dev/fast/)
