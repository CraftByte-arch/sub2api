import type { RouteRecordRaw } from 'vue-router'

export const extensionRoutes: RouteRecordRaw[] = [
  {
    path: '/image-studio',
    name: 'ImageStudio',
    component: () => import('@/extensions/image-studio/ImageStudioView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Image Studio',
      titleKey: 'imageStudio.title',
      descriptionKey: 'imageStudio.description'
    }
  }
]
