export type ImageStudioAssetKind = 'thumbnail' | 'original'

interface ImageStudioAssetCacheInput {
  jobId: number
  kind: ImageStudioAssetKind
  blob: Blob
}

export interface ImageStudioCachedAsset {
  jobId: number
  kind: ImageStudioAssetKind
  blob: Blob
  mimeType: string
  sizeBytes: number
  createdAt: number
  updatedAt: number
}

const dbName = 'sub2api-image-studio'
const dbVersion = 2
const assetStoreName = 'assets'

let dbPromise: Promise<IDBDatabase> | null = null
const memoryAssetCache = new Map<string, ImageStudioCachedAsset>()

export async function putImageStudioAssetCache(input: ImageStudioAssetCacheInput): Promise<void> {
  const now = Date.now()
  const existing = await getCachedImageStudioAsset(input.jobId, input.kind)
  const record: ImageStudioCachedAsset = {
    jobId: input.jobId,
    kind: input.kind,
    blob: input.blob,
    mimeType: input.blob.type || existing?.mimeType || '',
    sizeBytes: input.blob.size,
    createdAt: existing?.createdAt || now,
    updatedAt: now
  }
  if (typeof indexedDB === 'undefined') {
    memoryAssetCache.set(assetKey(input.jobId, input.kind), record)
    return
  }
  const db = await openImageStudioCacheDB()
  await runStoreRequest(db, 'readwrite', (store) => store.put(record, assetKey(input.jobId, input.kind)))
}

export async function getCachedImageStudioAsset(
  jobId: number,
  kind: ImageStudioAssetKind
): Promise<ImageStudioCachedAsset | null> {
  if (typeof indexedDB === 'undefined') {
    return memoryAssetCache.get(assetKey(jobId, kind)) || null
  }
  const db = await openImageStudioCacheDB()
  const record = await runStoreRequest<ImageStudioCachedAsset | undefined>(
    db,
    'readonly',
    (store) => store.get(assetKey(jobId, kind))
  )
  return record || null
}

export async function deleteImageStudioAssetCache(jobId: number): Promise<void> {
  if (typeof indexedDB === 'undefined') {
    memoryAssetCache.delete(assetKey(jobId, 'thumbnail'))
    memoryAssetCache.delete(assetKey(jobId, 'original'))
    return
  }
  const db = await openImageStudioCacheDB()
  await runTransaction(db, 'readwrite', (store) => {
    store.delete(assetKey(jobId, 'thumbnail'))
    store.delete(assetKey(jobId, 'original'))
  })
}

export async function clearImageStudioAssetCache(): Promise<number> {
  if (typeof indexedDB === 'undefined') {
    const removed = memoryAssetCache.size
    memoryAssetCache.clear()
    return removed
  }
  const db = await openImageStudioCacheDB()
  const keys = await runStoreRequest<IDBValidKey[]>(db, 'readonly', (store) => store.getAllKeys())
  await runStoreRequest(db, 'readwrite', (store) => store.clear())
  return keys.length
}

function openImageStudioCacheDB(): Promise<IDBDatabase> {
  if (typeof indexedDB === 'undefined') {
    return Promise.reject(new Error('IndexedDB is unavailable'))
  }
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve, reject) => {
    const request = indexedDB.open(dbName, dbVersion)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(assetStoreName)) {
        db.createObjectStore(assetStoreName)
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('Failed to open image studio cache'))
  })
  return dbPromise
}

function assetKey(jobId: number, kind: ImageStudioAssetKind): string {
  return `${jobId}:${kind}`
}

function runStoreRequest<T = unknown>(
  db: IDBDatabase,
  mode: IDBTransactionMode,
  action: (store: IDBObjectStore) => IDBRequest<T>
): Promise<T> {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(assetStoreName, mode)
    const request = action(transaction.objectStore(assetStoreName))
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('Image studio cache request failed'))
    transaction.onerror = () => reject(transaction.error || new Error('Image studio cache transaction failed'))
  })
}

function runTransaction(
  db: IDBDatabase,
  mode: IDBTransactionMode,
  action: (store: IDBObjectStore) => void
): Promise<void> {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(assetStoreName, mode)
    action(transaction.objectStore(assetStoreName))
    transaction.oncomplete = () => resolve()
    transaction.onerror = () => reject(transaction.error || new Error('Image studio cache transaction failed'))
  })
}
