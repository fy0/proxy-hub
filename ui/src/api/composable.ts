import type { WatchSource } from 'vue';
import { onBeforeRouteUpdate } from 'vue-router';
import { watch, onActivated } from 'vue';

// 初始化函数
// 一般用于初始化数据，例如从后端获取数据
// 如果挂上 watchSources，那么每次变更都会执行 init 函数
export function useInit(
  init: () => void,
  watchSources: WatchSource<unknown>[] = [],
  options: Parameters<typeof watch>[2] = {},
) {
  // 1) 第一次 + sources 变化
  watch(watchSources, () => init(), { ...options });

  // 2) 路由参数变化（组件被复用）
  onBeforeRouteUpdate(() => init());

  // 3) KeepAlive 切回
  onActivated(() => init());

  init();
}
