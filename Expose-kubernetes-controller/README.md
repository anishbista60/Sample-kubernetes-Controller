## This is sample kubernetes controller with exposes the application to the outside world 

### Functionality of this controller is : 
- When we create the deployment, controller automatically creates the service and ingress
- When we delete that deployment, controller will  delete that service and ingress. 
- When we  try to delete service or ingress explicitly without deleting deployment, controller maintain that state and recreate them. 
