import { create } from 'zustand'
import { persist } from 'zustand/middleware'

// 魔改：NavItem 去掉 'model'（原"价格"导航项已删除）
export type NavItem = 'home' | 'channel' | 'group' | 'log' | 'setting'

const NAV_ORDER: NavItem[] = ['home', 'channel', 'group', 'log', 'setting']

interface NavState {
    activeItem: NavItem
    prevItem: NavItem | null
    direction: number
    setActiveItem: (item: NavItem) => void
}

export const useNavStore = create<NavState>()(
    persist(
        (set, get) => ({
            activeItem: 'home',
            prevItem: null,
            direction: 0,
            setActiveItem: (item) => {
                const { activeItem } = get()
                const currentIndex = NAV_ORDER.indexOf(activeItem)
                const newIndex = NAV_ORDER.indexOf(item)
                const direction = newIndex > currentIndex ? 1 : -1

                set({
                    activeItem: item,
                    prevItem: activeItem,
                    direction
                })
            },
        }),
        {
            name: 'nav-storage',
            // 魔改兜底：若本地缓存里 activeItem 是已删除的 'model'(原价格页)，
            // 恢复时重置回主页，避免指向不存在的模块导致白屏
            merge: (persisted, current) => {
                const p = persisted as Partial<NavState> | undefined
                if (p?.activeItem && !(NAV_ORDER as string[]).includes(p.activeItem)) {
                    p.activeItem = 'home'
                }
                return { ...current, ...p }
            },
        }
    )
)