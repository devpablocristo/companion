import { createBrowserStorageNamespace } from '@devpablocristo/core-browser/storage'

const storage = createBrowserStorageNamespace({
  namespace: 'companion',
  hostAware: false,
})

export function getSavedView() {
  return storage.getString('tab') || 'home'
}

export function saveView(view) {
  storage.setString('tab', view)
}

export function getSavedLang() {
  return storage.getString('lang') || 'en'
}

export function saveLang(lang) {
  storage.setString('lang', lang)
}
