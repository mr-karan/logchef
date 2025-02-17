export interface User {
  id: string;
  email: string;
  full_name: string;
  role: "admin" | "member";
  status: "active" | "inactive";
  avatar?: string;
  last_login_at?: string;
  last_active_at?: string;
  created_at: string;
  updated_at: string;
}

export interface Session {
  id: string;
  user_id: string;
  expires_at: string;
  created_at: string;
}

export interface ApiResponse<T = any> {
  status: "success" | "error";
  data: T;
}
