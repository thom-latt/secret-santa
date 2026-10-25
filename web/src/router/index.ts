import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import HomeView from '../views/HomeView.vue'
import EventAdminView from '../views/EventAdminView.vue'
import ParticipantView from '../views/ParticipantView.vue'
import SuperAdminView from '../views/SuperAdminView.vue'

export const routes: RouteRecordRaw[] = [
  { path: '/', name: 'home', component: HomeView },
  { path: '/admin/events/:token', name: 'event-admin', component: EventAdminView },
  { path: '/p/:token', name: 'participant', component: ParticipantView },
  { path: '/superadmin', name: 'superadmin', component: SuperAdminView },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})
