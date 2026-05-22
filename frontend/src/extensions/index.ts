import type { RouteRecordRaw } from 'vue-router'

export const extensionRoutes: RouteRecordRaw[] = [
  {
    path: '/image-studio/template-preview',
    name: 'ImageStudioTemplatePreview',
    component: () => import('@/extensions/image-studio/TemplateDrawerPreview.vue'),
    meta: {
      requiresAuth: false,
      requiresAdmin: false,
      title: 'Template Drawer Preview'
    }
  },
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
