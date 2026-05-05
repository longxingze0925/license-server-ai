import { create } from 'zustand';
import { clearCSRFToken } from '../api/request';

interface User {
  id: string;
  email: string;
  name: string;
  role: 'owner' | 'admin' | 'developer' | 'viewer';
  avatar?: string;
  phone?: string;
}

interface Tenant {
  id: string;
  name: string;
  slug: string;
  plan: 'free' | 'pro' | 'enterprise';
  logo?: string;
}

interface AuthState {
  token: string | null;
  user: User | null;
  tenant: Tenant | null;
  isAuthenticated: boolean;
  setAuth: (token: string, user: User, tenant?: Tenant) => void;
  setTenant: (tenant: Tenant) => void;
  updateUser: (user: Partial<User>) => void;
  logout: () => void;
}

const readJSONStorage = <T,>(key: string): T | null => {
  const raw = localStorage.getItem(key);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as T;
  } catch {
    localStorage.removeItem(key);
    return null;
  }
};

const initialUser = readJSONStorage<User>('user');
const initialTenant = initialUser ? readJSONStorage<Tenant>('tenant') : null;
const initialToken = initialUser ? localStorage.getItem('token') : null;
if (!initialUser) {
  localStorage.removeItem('token');
  localStorage.removeItem('tenant');
}

export const useAuthStore = create<AuthState>((set) => ({
  token: initialToken,
  user: initialUser,
  tenant: initialTenant,
  isAuthenticated: !!initialToken,
  setAuth: (token, user, tenant) => {
    clearCSRFToken();
    localStorage.setItem('token', token);
    localStorage.setItem('user', JSON.stringify(user));
    if (tenant) {
      localStorage.setItem('tenant', JSON.stringify(tenant));
    }
    set({ token, user, tenant: tenant || null, isAuthenticated: true });
  },
  setTenant: (tenant) => {
    localStorage.setItem('tenant', JSON.stringify(tenant));
    set({ tenant });
  },
  updateUser: (userData) => {
    set((state) => {
      const newUser = state.user ? { ...state.user, ...userData } : null;
      if (newUser) {
        localStorage.setItem('user', JSON.stringify(newUser));
      }
      return { user: newUser };
    });
  },
  logout: () => {
    clearCSRFToken();
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    localStorage.removeItem('tenant');
    set({ token: null, user: null, tenant: null, isAuthenticated: false });
  },
}));
