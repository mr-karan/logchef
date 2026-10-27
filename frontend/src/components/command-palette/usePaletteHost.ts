import { nextTick, onMounted, onScopeDispose, shallowRef, watch, type Component, type ShallowRef } from 'vue';
import { useRouter } from 'vue-router';
import { useAuthStore } from '@/stores/auth';

type HostState = { readonly kind: 'closed' | 'loading' | 'open' };

export function usePaletteHost() {
  const router = useRouter();
  const auth = useAuthStore();
  const component: ShallowRef<Component | null> = shallowRef(null);
  const state = shallowRef<HostState>({ kind: 'closed' });
  const error = shallowRef(false);
  let dismissal = new AbortController();
  const cancellation = shallowRef(dismissal.signal);
  let generation = 0;
  let opener: HTMLElement | null = null;

  function close(restore = true) {
    if (restore) dismissal.abort();
    generation++;
    state.value = { kind: 'closed' };
    if (restore) void nextTick(() => { if (opener?.isConnected) opener.focus(); });
  }

  async function open() {
    if (!auth.user || state.value.kind !== 'closed') return;
    dismissal = new AbortController();
    cancellation.value = dismissal.signal;
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    error.value = false;
    if (component.value) {
      state.value = { kind: 'open' };
      return;
    }
    state.value = { kind: 'loading' };
    const attempt = ++generation;
    try {
      const loaded = await import('./CommandPalette.vue');
      if (generation !== attempt) return;
      component.value = loaded.default;
      state.value = { kind: 'open' };
    } catch {
      if (generation !== attempt) return;
      state.value = { kind: 'closed' };
      error.value = true;
    }
  }

  function keydown(event: KeyboardEvent) {
    if (event.isComposing || event.repeat) return;
    if ((event.metaKey || event.ctrlKey) && !event.altKey && (event.key.toLowerCase() === 'k' || (event.code === 'KeyK' && !/^[a-z]$/i.test(event.key)))) {
      event.preventDefault();
      event.stopImmediatePropagation();
      if (state.value.kind === 'closed') void open();
      else close();
    } else if (event.key === 'Escape' && state.value.kind !== 'closed') {
      event.preventDefault();
      event.stopImmediatePropagation();
      close();
    }
  }

  const removeNavigation = router.afterEach((to, from, failure) => { if (!failure && to.path !== from.path) close(false); });
  watch(() => auth.user?.id, () => {
    close(false);
    component.value = null;
  });
  onMounted(() => window.addEventListener('keydown', keydown, true));
  onScopeDispose(() => {
    dismissal.abort();
    generation++;
    window.removeEventListener('keydown', keydown, true);
    removeNavigation();
  });
  return { component, state, error, cancellation, open, close };
}
