import api from './api'

export const checkNeedsSetup = async () => {
  const response = await api.get('/setup/status')
  return Boolean(response?.data?.needs_setup)
}
