angular.module('syncthingfuse.pins')
    .directive('editPinsModal', function () {
        return {
            restrict: 'A',
            templateUrl: 'js/pins/editPinsModalView.html'
        };
});
